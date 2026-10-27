<?php
// Backupper — temporary backup script.
// The agent uploads it over FTP/SFTP for a single backup run and deletes it right after.
// Every request must carry an HMAC signature made with the secret below; anything else
// gets a plain 404 page, so the file looks like it does not exist.
// Responses are encrypted with AES-256-GCM, so the dump is protected even over plain HTTP.

error_reporting(0);
@ini_set('display_errors', '0');

const BK_SECRET = '__BK_SECRET__';
const BK_VERSION = 1;

function bk_404()
{
    http_response_code(404);
    header('Content-Type: text/html; charset=utf-8');
    echo "<!DOCTYPE html>\n<html><head><title>404 Not Found</title></head><body><h1>Not Found</h1>"
        . "<p>The requested URL was not found on this server.</p></body></html>";
    exit;
}

if (($_SERVER['REQUEST_METHOD'] ?? '') !== 'POST' || strlen(BK_SECRET) !== 64) {
    bk_404();
}
$bkAction = (string)($_POST['a'] ?? '');
$bkParams = (string)($_POST['p'] ?? '');
$bkTime = (string)($_POST['t'] ?? '');
$bkNonce = (string)($_POST['n'] ?? '');
$bkSig = (string)($_POST['s'] ?? '');
$bkMac = hash_hmac('sha256', "$bkAction|$bkTime|$bkNonce|$bkParams", hash('sha256', 'mac' . BK_SECRET, true));
if (!hash_equals($bkMac, $bkSig) || abs(time() - (int)$bkTime) > 900) {
    bk_404();
}

@set_time_limit(0);
@ignore_user_abort(false);
while (ob_get_level()) {
    @ob_end_clean();
}

$GLOBALS['bk_sent'] = false;

function bk_send(array $meta, $data = '')
{
    $GLOBALS['bk_sent'] = true;
    $json = json_encode($meta, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_PARTIAL_OUTPUT_ON_ERROR);
    $plain = pack('N', strlen($json)) . $json . $data;
    $key = hash('sha256', 'enc' . BK_SECRET, true);
    $iv = random_bytes(12);
    $tag = '';
    $ct = openssl_encrypt($plain, 'aes-256-gcm', $key, OPENSSL_RAW_DATA, $iv, $tag, '', 16);
    unset($plain);
    header('Content-Type: application/octet-stream');
    header('Cache-Control: no-store, no-transform');
    header('X-Accel-Buffering: no');
    header('Content-Length: ' . (12 + strlen($ct) + 16));
    echo $iv, $ct, $tag;
    exit;
}

register_shutdown_function(function () {
    $e = error_get_last();
    if (!$GLOBALS['bk_sent'] && $e && in_array($e['type'], [E_ERROR, E_CORE_ERROR, E_COMPILE_ERROR, E_USER_ERROR], true)) {
        bk_send(['err' => 'PHP: ' . $e['message']]);
    }
});

if (!function_exists('openssl_encrypt')) {
    http_response_code(500);
    echo 'Backupper: openssl extension is required';
    exit;
}

/** Collects output, compressing it into one gzip member per response. */
class BkOut
{
    public $raw = 0;
    private $z;
    private $buf = '';
    private $plain = '';

    public function __construct()
    {
        $this->z = function_exists('deflate_init') ? deflate_init(ZLIB_ENCODING_GZIP, ['level' => 6]) : null;
    }

    public function w($s)
    {
        $this->raw += strlen($s);
        if ($this->z) {
            $this->buf .= deflate_add($this->z, $s, ZLIB_NO_FLUSH);
        } else {
            $this->plain .= $s;
        }
    }

    public function size()
    {
        return $this->z ? strlen($this->buf) : (int)(strlen($this->plain) / 3);
    }

    public function finish()
    {
        if ($this->raw === 0) {
            return '';
        }
        if ($this->z) {
            return $this->buf . deflate_add($this->z, '', ZLIB_FINISH);
        }
        return gzencode($this->plain, 6);
    }
}

class BkBudget
{
    private $start;
    private $sec;
    private $max;

    public function __construct($sec, $max)
    {
        $this->start = microtime(true);
        $this->sec = $sec;
        $this->max = $max;
    }

    public function over(BkOut $out)
    {
        return microtime(true) - $this->start > $this->sec || $out->size() >= $this->max || $out->raw >= $this->max * 6;
    }
}

function bk_bytes($v)
{
    $v = trim((string)$v);
    if ($v === '' || $v === '-1') {
        return -1;
    }
    $n = (float)$v;
    switch (strtolower(substr($v, -1))) {
        case 'g':
            $n *= 1024;
        // no break
        case 'm':
            $n *= 1024;
        // no break
        case 'k':
            $n *= 1024;
    }
    return (int)$n;
}

// ---------- CMS and database settings ----------

/**
 * Where popular CMS keep the database access, relative to the site root ("../" — one
 * level up, e.g. a Laravel or Magento root opened at public/ or pub/). The first CMS
 * with an existing file is the site's CMS.
 */
function bk_cms_configs()
{
    return [
        'bitrix' => ['bitrix/.settings.php', 'bitrix/php_interface/dbconn.php'],
        'wordpress' => ['wp-config.php', '../wp-config.php'],
        'joomla' => ['configuration.php'],
        'drupal' => ['sites/default/settings.php'],
        'opencart' => ['admin/config.php'],
        'modx' => ['config.core.php', 'core/config/config.inc.php'],
        'prestashop' => ['app/config/parameters.php', 'config/settings.inc.php'],
        'magento' => ['app/etc/env.php', '../app/etc/env.php'],
        'dle' => ['engine/data/dbconfig.php'],
        'hostcms' => ['modules/core/config/database.php'],
        'webasyst' => ['wa-config/db.php'],
        'umi' => ['config.ini'],
        'laravel' => ['../artisan', 'artisan'],
    ];
}

function bk_cms($root)
{
    foreach (bk_cms_configs() as $cms => $files) {
        foreach ($files as $f) {
            if (is_file("$root/$f")) {
                return $cms;
            }
        }
    }
    return '';
}

/** PHP source without comments, so commented-out examples are not taken for settings. */
function bk_php_code($f)
{
    $src = (string)@file_get_contents($f);
    if ($src === '' || !function_exists('token_get_all')) {
        return $src;
    }
    $out = '';
    foreach (@token_get_all($src) as $tk) {
        if (!is_array($tk)) {
            $out .= $tk;
        } elseif ($tk[0] !== T_COMMENT && $tk[0] !== T_DOC_COMMENT) {
            $out .= $tk[1];
        }
    }
    return $out;
}

/** First match of $lead followed by a quoted string or a number, unescaped. */
function bk_php_value($src, $lead)
{
    if (!preg_match('/' . $lead . '\s*(?:\'((?:[^\'\\\\]|\\\\.)*)\'|"((?:[^"\\\\]|\\\\.)*)"|(\d+))/s', $src, $m)) {
        return null;
    }
    if (isset($m[3]) && $m[3] !== '') {
        return $m[3];
    }
    if (isset($m[2]) && $m[2] !== '') {
        return stripcslashes($m[2]);
    }
    return str_replace(["\\'", '\\\\'], ["'", '\\'], $m[1]);
}

/** define('NAME', 'value') */
function bk_php_string($src, $name)
{
    return bk_php_value($src, 'define\s*\(\s*[\'"]' . preg_quote($name, '/') . '[\'"]\s*,');
}

/** $name = 'value' (also "public $name = 'value'" in a class) */
function bk_php_var($src, $name)
{
    return bk_php_value($src, '\$' . preg_quote($name, '/') . '\s*=');
}

/** 'key' => 'value' */
function bk_php_key($src, $key)
{
    return bk_php_value($src, '[\'"]' . preg_quote($key, '/') . '[\'"]\s*=>');
}

/** KEY=value lines of a .env file. */
function bk_env($f)
{
    $r = [];
    foreach ((array)@file($f, FILE_IGNORE_NEW_LINES) as $l) {
        if (!preg_match('/^\s*(?:export\s+)?([A-Za-z0-9_.]+)\s*=\s*(.*)$/', $l, $m)) {
            continue;
        }
        $v = trim($m[2]);
        if (preg_match('/^"((?:[^"\\\\]|\\\\.)*)"/', $v, $q)) {
            $v = stripcslashes($q[1]);
        } elseif (preg_match("/^'([^']*)'/", $v, $q)) {
            $v = $q[1];
        } else {
            $v = trim(preg_replace('/\s+[#;].*$/', '', $v));
        }
        $r[$m[1]] = $v;
    }
    return $r;
}

/** The settings found in $file (relative to the root) as host/name/user/pass/from, or null. */
function bk_db_found($file, $host, $name, $user, $pass, $port = null)
{
    if ($name === null || $name === '') {
        return null;
    }
    $host = (string)$host;
    if ($port !== null && $port !== '' && (string)$port !== '3306' && strpos($host, ':') === false) {
        $host = ($host === '' ? 'localhost' : $host) . ':' . $port;
    }
    return ['host' => $host, 'name' => (string)$name, 'user' => (string)$user, 'pass' => (string)$pass, 'from' => $file];
}

function bk_db_settings($root, $cms)
{
    $up = dirname($root);
    $first = function (array $files) use ($root) {
        foreach ($files as $f) {
            if (is_file("$root/$f")) {
                return $f;
            }
        }
        return null;
    };
    switch ($cms) {
        case 'bitrix':
            $f = "$root/bitrix/.settings.php";
            if (is_file($f)) {
                $s = include $f;
                $c = $s['connections']['value']['default'] ?? null;
                if (is_array($c) && !empty($c['database'])) {
                    return bk_db_found('bitrix/.settings.php', $c['host'] ?? '', $c['database'], $c['login'] ?? '', $c['password'] ?? '');
                }
            }
            $f = "$root/bitrix/php_interface/dbconn.php";
            if (is_file($f)) {
                $r = (function ($f) {
                    $DBHost = $DBLogin = $DBPassword = $DBName = null;
                    @include $f;
                    return [$DBHost, $DBName, $DBLogin, $DBPassword];
                })($f);
                return bk_db_found('bitrix/php_interface/dbconn.php', $r[0], $r[1], $r[2], $r[3]);
            }
            return null;
        case 'wordpress':
            $f = $first(['wp-config.php', '../wp-config.php']);
            $src = $f ? bk_php_code("$root/$f") : '';
            return bk_db_found('wp-config.php', bk_php_string($src, 'DB_HOST'), bk_php_string($src, 'DB_NAME'), bk_php_string($src, 'DB_USER'), bk_php_string($src, 'DB_PASSWORD'));
        case 'joomla':
            $src = bk_php_code("$root/configuration.php");
            if (preg_match('/^(pg|postgres)/i', (string)bk_php_var($src, 'dbtype'))) {
                return ['error' => 'configuration.php: сайт работает на PostgreSQL, а сохранять можно только MySQL/MariaDB'];
            }
            return bk_db_found('configuration.php', bk_php_var($src, 'host'), bk_php_var($src, 'db'), bk_php_var($src, 'user'), bk_php_var($src, 'password'));
        case 'drupal':
            $src = bk_php_code("$root/sites/default/settings.php");
            if (($url = bk_php_var($src, 'db_url')) !== null && ($u = parse_url($url))) { // Drupal 6
                return bk_db_found('sites/default/settings.php', $u['host'] ?? '', trim($u['path'] ?? '', '/'), urldecode($u['user'] ?? ''), urldecode($u['pass'] ?? ''), $u['port'] ?? null);
            }
            if (preg_match('/^(pgsql|sqlite)/', (string)bk_php_key($src, 'driver'))) {
                return ['error' => 'sites/default/settings.php: база не MySQL/MariaDB'];
            }
            return bk_db_found('sites/default/settings.php', bk_php_key($src, 'host'), bk_php_key($src, 'database'), bk_php_key($src, 'username'), bk_php_key($src, 'password'), bk_php_key($src, 'port'));
        case 'opencart':
            $f = $first(['config.php', 'admin/config.php']);
            $src = $f ? bk_php_code("$root/$f") : '';
            return bk_db_found((string)$f, bk_php_string($src, 'DB_HOSTNAME'), bk_php_string($src, 'DB_DATABASE'), bk_php_string($src, 'DB_USERNAME'), bk_php_string($src, 'DB_PASSWORD'), bk_php_string($src, 'DB_PORT'));
        case 'modx':
            // The core folder may be moved out of the site: config.core.php points to it.
            $core = "$root/core/";
            if (is_file("$root/config.core.php") && ($c = bk_php_string(bk_php_code("$root/config.core.php"), 'MODX_CORE_PATH'))) {
                $core = $c;
            }
            $f = rtrim($core, '/') . '/config/config.inc.php';
            $src = bk_php_code($f);
            $label = strpos($f, $root . '/') === 0 ? substr($f, strlen($root) + 1) : $f;
            return bk_db_found($label, bk_php_var($src, 'database_server'), trim((string)bk_php_var($src, 'dbase'), '`'), bk_php_var($src, 'database_user'), bk_php_var($src, 'database_password'));
        case 'prestashop':
            if (is_file("$root/app/config/parameters.php")) {
                $src = bk_php_code("$root/app/config/parameters.php");
                return bk_db_found('app/config/parameters.php', bk_php_key($src, 'database_host'), bk_php_key($src, 'database_name'), bk_php_key($src, 'database_user'), bk_php_key($src, 'database_password'), bk_php_key($src, 'database_port'));
            }
            $src = bk_php_code("$root/config/settings.inc.php");
            return bk_db_found('config/settings.inc.php', bk_php_string($src, '_DB_SERVER_'), bk_php_string($src, '_DB_NAME_'), bk_php_string($src, '_DB_USER_'), bk_php_string($src, '_DB_PASSWD_'));
        case 'magento':
            $f = $first(['app/etc/env.php', '../app/etc/env.php']);
            $src = $f ? bk_php_code("$root/$f") : '';
            return bk_db_found((string)$f, bk_php_key($src, 'host'), bk_php_key($src, 'dbname'), bk_php_key($src, 'username'), bk_php_key($src, 'password'));
        case 'dle':
            $src = bk_php_code("$root/engine/data/dbconfig.php");
            return bk_db_found('engine/data/dbconfig.php', bk_php_string($src, 'DBHOST'), bk_php_string($src, 'DBNAME'), bk_php_string($src, 'DBUSER'), bk_php_string($src, 'DBPASS'));
        case 'hostcms':
            $src = bk_php_code("$root/modules/core/config/database.php");
            return bk_db_found('modules/core/config/database.php', bk_php_key($src, 'host'), bk_php_key($src, 'database'), bk_php_key($src, 'username'), bk_php_key($src, 'password'));
        case 'webasyst':
            $src = bk_php_code("$root/wa-config/db.php");
            return bk_db_found('wa-config/db.php', bk_php_key($src, 'host'), bk_php_key($src, 'database'), bk_php_key($src, 'user'), bk_php_key($src, 'password'), bk_php_key($src, 'port'));
        case 'umi':
            $e = bk_env("$root/config.ini");
            return bk_db_found('config.ini', $e['core.host'] ?? '', $e['core.dbname'] ?? null, $e['core.login'] ?? '', $e['core.password'] ?? '', $e['core.port'] ?? null);
        case 'laravel':
            $f = is_file("$root/.env") ? "$root/.env" : "$up/.env";
            $e = bk_env($f);
            if (!in_array($e['DB_CONNECTION'] ?? 'mysql', ['mysql', 'mariadb'], true)) {
                return ['error' => '.env: DB_CONNECTION=' . $e['DB_CONNECTION'] . ', а сохранять можно только MySQL/MariaDB'];
            }
            $host = !empty($e['DB_SOCKET']) ? 'localhost:' . $e['DB_SOCKET'] : ($e['DB_HOST'] ?? '');
            return bk_db_found($f === "$root/.env" ? '.env' : '../.env', $host, $e['DB_DATABASE'] ?? null, $e['DB_USERNAME'] ?? '', $e['DB_PASSWORD'] ?? '', $e['DB_PORT'] ?? null);
    }
    return null;
}

/**
 * Connects to the site database: with the CMS settings first, then with the ones
 * entered in the panel when the CMS has none or they do not work.
 * Returns [settings, PDO, charset, error]; settings get a "note" when the panel's were used.
 */
function bk_db_open($root, array $p)
{
    $cms = ($p['cms'] ?? 'auto') === 'auto' ? bk_cms($root) : (string)$p['cms'];
    // After "info" the agent passes which settings worked, so the dump portions do
    // not wait for a failing connection again.
    $only = (string)($p['dbFrom'] ?? '');
    if ($only === 'panel') {
        $cms = 'none';
    }
    $tries = [];
    $errs = [];
    $cfg = $cms !== '' && $cms !== 'none' ? bk_db_settings($root, $cms) : null;
    if (isset($cfg['error'])) {
        $errs[] = $cfg['error'];
    } elseif ($cfg) {
        $tries[] = $cfg;
    } elseif ($cms !== '' && $cms !== 'none') {
        $errs[] = 'в настройках CMS доступ к базе не найден';
    }
    $o = $p['db'] ?? [];
    if (!empty($o['name']) && $only !== 'cms') {
        $tries[] = ['host' => (string)($o['host'] ?? 'localhost'), 'name' => (string)$o['name'], 'user' => (string)($o['user'] ?? ''), 'pass' => (string)($o['pass'] ?? ''), 'from' => 'панель'];
    }
    foreach ($tries as $db) {
        try {
            list($pdo, $cs) = bk_pdo($db);
            if ($errs && $db['from'] === 'панель') {
                $db['note'] = implode('; ', $errs);
            }
            return [$db, $pdo, $cs, null];
        } catch (Exception $e) {
            $errs[] = ($db['from'] === 'панель' ? 'доступ из панели' : $db['from']) . ': ' . $e->getMessage();
        }
    }
    if (!$tries) {
        return [null, null, null, 'не найдены настройки базы данных в файлах CMS — укажите доступ вручную'];
    }
    return [$tries[0], null, null, implode('; ', $errs)];
}

function bk_pdo(array $db)
{
    $host = $db['host'] !== '' ? $db['host'] : 'localhost';
    if (strpos($host, 'p:') === 0) {
        $host = substr($host, 2);
    }
    $dsn = 'mysql:dbname=' . $db['name'];
    $port = null;
    $sock = null;
    if (strpos($host, ':') !== false) {
        list($h, $rest) = explode(':', $host, 2);
        if (ctype_digit($rest)) {
            $port = $rest;
        } else {
            $sock = $rest;
        }
        $host = $h;
    }
    $dsn .= $sock ? ';unix_socket=' . $sock : ';host=' . $host . ($port ? ';port=' . $port : '');
    // A short connect timeout: settings that do not work give way to the next ones.
    $opts = [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION, PDO::ATTR_EMULATE_PREPARES => true, PDO::ATTR_TIMEOUT => 15];
    $last = null;
    foreach (['utf8mb4', 'utf8'] as $cs) {
        try {
            $pdo = new PDO($dsn . ';charset=' . $cs, $db['user'], $db['pass'], $opts);
            $pdo->exec("SET NAMES $cs");
            $pdo->exec("SET SESSION sql_mode=''");
            return [$pdo, $cs];
        } catch (Exception $e) {
            $last = $e;
        }
    }
    throw $last;
}

function bk_q($id)
{
    return '`' . str_replace('`', '``', $id) . '`';
}

// ---------- actions ----------

function bk_info($root, array $p)
{
    $cms = bk_cms($root);
    $limit = ini_get('max_execution_time');
    $res = [
        'version' => BK_VERSION,
        'php' => PHP_VERSION,
        'sapi' => PHP_SAPI,
        'os' => PHP_OS,
        'root' => $root,
        'cms' => $cms,
        'timeLimit' => (int)$limit,
        'memoryLimit' => bk_bytes(ini_get('memory_limit')),
        'diskFree' => (float)@disk_free_space($root),
        'deflate' => function_exists('deflate_init'),
        'pdoMysql' => class_exists('PDO') && in_array('mysql', PDO::getAvailableDrivers(), true),
        'serverTime' => time(),
        'top' => array_values(array_diff((array)@scandir($root), ['.', '..'])),
    ];
    $res['top'] = array_slice(array_values(array_filter($res['top'], function ($n) {
        return strpos($n, 'backupper_') !== 0;
    })), 0, 40);
    list($db, $pdo, , $err) = $res['pdoMysql'] ? bk_db_open($root, $p) : [null, null, null, null];
    if ($db) {
        $res['db'] = ['host' => $db['host'], 'name' => $db['name'], 'user' => $db['user'], 'from' => $db['from'], 'note' => $db['note'] ?? ''];
        try {
            if (!$pdo) {
                throw new Exception($err);
            }
            $st = $pdo->prepare('SELECT COUNT(*), COALESCE(SUM(data_length + index_length), 0) FROM information_schema.TABLES WHERE table_schema = ?');
            $st->execute([$db['name']]);
            $r = $st->fetch(PDO::FETCH_NUM);
            $res['db']['tables'] = (int)$r[0];
            $res['db']['size'] = (float)$r[1];
            $res['db']['server'] = $pdo->getAttribute(PDO::ATTR_SERVER_VERSION);
        } catch (Exception $e) {
            $res['db']['error'] = $e->getMessage();
        }
    } elseif ($err) {
        $res['dbError'] = $err;
    }
    bk_send(['info' => $res, 'done' => true]);
}

function bk_dump($root, array $p, BkBudget $budget)
{
    list($db, $pdo, $cs, $err) = bk_db_open($root, $p);
    if (!$pdo) {
        bk_send(['err' => $err]);
    }
    $st = $p['state'] ?? ['ph' => 'head'];
    $out = new BkOut();
    $log = [];

    $tables = [];
    $views = [];
    foreach ($pdo->query('SHOW FULL TABLES')->fetchAll(PDO::FETCH_NUM) as $r) {
        if (strtoupper($r[1]) === 'VIEW') {
            $views[] = $r[0];
        } else {
            $tables[] = $r[0];
        }
    }
    sort($tables);
    sort($views);

    if ($st['ph'] === 'head') {
        $out->w("-- Backupper dump of `{$db['name']}`, " . date('Y-m-d H:i:s') . "\n"
            . "-- Server: " . $pdo->getAttribute(PDO::ATTR_SERVER_VERSION) . ", PHP " . PHP_VERSION . "\n\n"
            . "SET NAMES $cs;\nSET FOREIGN_KEY_CHECKS=0;\nSET UNIQUE_CHECKS=0;\nSET SQL_MODE='NO_AUTO_VALUE_ON_ZERO';\n");
        $st = ['ph' => 'tables', 't' => 0, 'cr' => false, 'k' => null, 'o' => 0, 'lim' => 500];
        $log[] = 'таблиц: ' . count($tables) . ', представлений: ' . count($views);
    }

    while ($st['ph'] === 'tables' && !$budget->over($out)) {
        if ($st['t'] >= count($tables)) {
            $st = ['ph' => 'views'];
            break;
        }
        $tbl = $tables[$st['t']];
        if (!$st['cr']) {
            $row = $pdo->query('SHOW CREATE TABLE ' . bk_q($tbl))->fetch(PDO::FETCH_NUM);
            $out->w("\n--\n-- Table " . bk_q($tbl) . "\n--\nDROP TABLE IF EXISTS " . bk_q($tbl) . ";\n" . $row[1] . ";\n");
            $pk = null;
            $keys = $pdo->query('SHOW KEYS FROM ' . bk_q($tbl) . " WHERE Key_name = 'PRIMARY'")->fetchAll(PDO::FETCH_ASSOC);
            if (count($keys) === 1) {
                foreach ($pdo->query('SHOW COLUMNS FROM ' . bk_q($tbl))->fetchAll(PDO::FETCH_ASSOC) as $c) {
                    if ($c['Field'] === $keys[0]['Column_name'] && preg_match('/int/i', $c['Type'])) {
                        $pk = $c['Field'];
                    }
                }
            }
            $st['cr'] = true;
            $st['pk'] = $pk;
            $st['k'] = null;
            $st['o'] = 0;
        }
        $lim = max(5, min(5000, (int)$st['lim']));
        if ($st['pk'] !== null) {
            $sql = 'SELECT * FROM ' . bk_q($tbl) . ($st['k'] !== null ? ' WHERE ' . bk_q($st['pk']) . ' > ' . $pdo->quote((string)$st['k']) : '')
                . ' ORDER BY ' . bk_q($st['pk']) . " LIMIT $lim";
        } else {
            $sql = 'SELECT * FROM ' . bk_q($tbl) . " LIMIT {$st['o']}, $lim";
        }
        $rows = $pdo->query($sql)->fetchAll(PDO::FETCH_ASSOC);
        if ($rows) {
            $cols = '(' . implode(',', array_map('bk_q', array_keys($rows[0]))) . ')';
            $head = 'INSERT INTO ' . bk_q($tbl) . " $cols VALUES\n";
            $stmt = '';
            $bytes = 0;
            foreach ($rows as $r) {
                $vals = [];
                foreach ($r as $v) {
                    if ($v === null) {
                        $vals[] = 'NULL';
                    } elseif ($v === '') {
                        $vals[] = "''";
                    } elseif (!preg_match('//u', $v)) {
                        $vals[] = '0x' . bin2hex($v);
                    } else {
                        $vals[] = $pdo->quote($v);
                    }
                }
                $line = '(' . implode(',', $vals) . ')';
                $bytes += strlen($line);
                if ($stmt !== '' && strlen($stmt) + strlen($line) > 1000000) {
                    $out->w($head . $stmt . ";\n");
                    $stmt = '';
                }
                $stmt .= ($stmt === '' ? '' : ",\n") . $line;
            }
            if ($stmt !== '') {
                $out->w($head . $stmt . ";\n");
            }
            // Aim for ~2 MB per batch so tables with big rows do not exhaust memory.
            $avg = max(1, $bytes / count($rows));
            $st['lim'] = (int)max(5, min(5000, 2000000 / $avg));
            if ($st['pk'] !== null) {
                $st['k'] = $rows[count($rows) - 1][$st['pk']];
            } else {
                $st['o'] += count($rows);
            }
        }
        if (count($rows) < $lim) {
            $st['t']++;
            $st['cr'] = false;
        }
    }

    if ($st['ph'] === 'views') {
        foreach ($views as $v) {
            try {
                $row = $pdo->query('SHOW CREATE VIEW ' . bk_q($v))->fetch(PDO::FETCH_NUM);
                $def = preg_replace('/DEFINER=`[^`]*`@`[^`]*`\s*/', '', $row[1]);
                $out->w("\nDROP VIEW IF EXISTS " . bk_q($v) . ";\n$def;\n");
            } catch (Exception $e) {
                $log[] = "представление $v пропущено: " . $e->getMessage();
            }
        }
        $out->w("\nSET FOREIGN_KEY_CHECKS=1;\nSET UNIQUE_CHECKS=1;\n-- end of dump\n");
        $st = ['ph' => 'done'];
    }

    $raw = $out->raw;
    bk_send(['state' => $st, 'done' => $st['ph'] === 'done', 'raw' => $raw, 'log' => $log], $out->finish());
}

// ---------- files: tar stream built across requests ----------

/** Folders of the presets (sent by the agent) whose CMS is detected in the site root. */
function bk_cms_excludes($root, $cms, array $presets, array &$log)
{
    $out = [];
    foreach ($presets as $pr) {
        $hit = ($pr['id'] ?? '') === $cms;
        foreach ((array)($pr['markers'] ?? []) as $m) {
            $hit = $hit || is_file("$root/$m");
        }
        if ($hit) {
            $out = array_merge($out, (array)($pr['excludes'] ?? []));
            $log[] = 'CMS ' . $pr['name'] . ': кэш и резервные копии не попадут в архив';
        }
    }
    return $out;
}

/** User patterns without "/" match a name anywhere; CMS folders match from the root only. */
function bk_excluded($rel, $base, array $pats, array $anchored)
{
    foreach ($pats as $p) {
        if (strpos($p, '/') === false) {
            if (fnmatch($p, $base)) {
                return true;
            }
        } elseif (fnmatch($p, $rel)) {
            return true;
        }
    }
    foreach ($anchored as $p) {
        if (fnmatch($p, $rel)) {
            return true;
        }
    }
    return false;
}

function bk_list_path($root)
{
    return $root . '/' . basename(__FILE__, '.php') . '.list.php';
}

function bk_build_list($root, array $pats, array $anchored)
{
    $f = fopen(bk_list_path($root), 'wb');
    if (!$f) {
        bk_send(['err' => 'не удалось создать временный список файлов в корне сайта (нет прав на запись?)']);
    }
    // The guard line keeps the list unreadable over the web while it exists.
    fwrite($f, "<?php exit; ?>\n");
    $count = 0;
    $skip = 0;
    $tar = 1024.0; // expected tar size (float: 32-bit PHP), for the progress estimate
    $it = new RecursiveIteratorIterator(
        new RecursiveCallbackFilterIterator(
            new RecursiveDirectoryIterator($root, FilesystemIterator::SKIP_DOTS | FilesystemIterator::UNIX_PATHS | FilesystemIterator::CURRENT_AS_FILEINFO),
            function ($fi) use ($root, $pats, $anchored, &$skip) {
                $rel = ltrim(substr(str_replace('\\', '/', $fi->getPathname()), strlen($root)), '/');
                if (strpos($fi->getFilename(), 'backupper_') === 0 && strpos($rel, '/') === false) {
                    return false;
                }
                if (bk_excluded($rel, $fi->getFilename(), $pats, $anchored)) {
                    $skip++;
                    return false;
                }
                return true;
            }
        ),
        RecursiveIteratorIterator::SELF_FIRST,
        RecursiveIteratorIterator::CATCH_GET_CHILD
    );
    foreach ($it as $fi) {
        $rel = ltrim(substr(str_replace('\\', '/', $fi->getPathname()), strlen($root)), '/');
        if (strpos($rel, "\n") !== false) {
            continue;
        }
        $t = $fi->isLink() ? 'l' : ($fi->isDir() ? 'd' : 'f');
        fwrite($f, "$t\t$rel\n");
        $count++;
        $tar += bk_tar_head_size($t === 'd' ? $rel . '/' : $rel);
        if ($t === 'f') {
            $tar += ceil((float)@filesize($fi->getPathname()) / 512) * 512;
        }
    }
    fclose($f);
    return [$count, $skip, $tar];
}

// Bytes taken by the headers of one entry, as bk_tar_header writes them.
function bk_tar_head_size($name)
{
    $n = strlen($name);
    return 512 + ($n > 99 ? 512 + ceil(($n + 1) / 512) * 512 : 0);
}

function bk_tar_num($n, $len)
{
    if ($n >= pow(8, $len - 1)) {
        // base-256 for files of 8 GB and more
        return "\x80" . str_repeat("\0", $len - 9) . pack('J', $n);
    }
    return str_pad(decoct($n), $len - 1, '0', STR_PAD_LEFT) . "\0";
}

function bk_tar_block($name, $size, $mode, $mtime, $type, $link = '')
{
    $h = str_pad($name, 100, "\0") . bk_tar_num($mode & 07777, 8) . bk_tar_num(0, 8) . bk_tar_num(0, 8)
        . bk_tar_num($size, 12) . bk_tar_num($mtime, 12) . '        ' . $type . str_pad($link, 100, "\0")
        . "ustar\0" . '00' . str_repeat("\0", 32 + 32 + 8 + 8 + 155 + 12);
    $sum = 0;
    for ($i = 0; $i < 512; $i++) {
        $sum += ord($h[$i]);
    }
    return substr_replace($h, str_pad(decoct($sum), 6, '0', STR_PAD_LEFT) . "\0 ", 148, 8);
}

function bk_tar_header($name, $size, $mode, $mtime, $type, $link = '')
{
    $out = '';
    if (strlen($name) > 99) {
        $out .= bk_tar_block('././@LongLink', strlen($name) + 1, 0644, 0, 'L') . str_pad($name . "\0", (int)ceil((strlen($name) + 1) / 512) * 512, "\0");
        $name = substr($name, 0, 99);
    }
    if (strlen($link) > 99) {
        $out .= bk_tar_block('././@LongLink', strlen($link) + 1, 0644, 0, 'K') . str_pad($link . "\0", (int)ceil((strlen($link) + 1) / 512) * 512, "\0");
        $link = substr($link, 0, 99);
    }
    return $out . bk_tar_block($name, $size, $mode, $mtime, $type, $link);
}

function bk_files($root, array $p, BkBudget $budget)
{
    $st = $p['state'] ?? ['ph' => 'list'];
    $out = new BkOut();
    $log = [];
    if ($st['ph'] === 'list') {
        $cms = ($p['cms'] ?? 'auto') === 'auto' ? bk_cms($root) : (string)$p['cms'];
        $pats = array_values(array_filter(array_map(function ($x) {
            return trim(str_replace('\\', '/', $x), " /");
        }, (array)($p['exclude'] ?? []))));
        $anchored = [];
        if (!empty($p['cmsExcludes'])) {
            $anchored = bk_cms_excludes($root, $cms, (array)($p['presets'] ?? []), $log);
        }
        list($count, $skip, $tar) = bk_build_list($root, $pats, $anchored);
        $log[] = "в архив попадёт объектов: $count, исключено по шаблонам: $skip";
        $st = ['ph' => 'tar', 'lo' => 0, 'off' => 0, 'sz' => 0, 'n' => 0, 'skip' => 0];
        bk_send(['state' => $st, 'done' => false, 'raw' => 0, 'est' => $tar, 'log' => $log]);
    }

    $lf = fopen(bk_list_path($root), 'rb');
    if (!$lf) {
        bk_send(['err' => 'временный список файлов пропал — запустите бэкап заново']);
    }
    if ($st['lo'] === 0) {
        fgets($lf); // guard line
    } else {
        fseek($lf, $st['lo']);
    }
    while ($st['ph'] === 'tar' && !$budget->over($out)) {
        $pos = ftell($lf);
        $line = fgets($lf);
        if ($line === false) {
            $out->w(str_repeat("\0", 1024));
            $st['ph'] = 'done';
            break;
        }
        list($t, $rel) = explode("\t", rtrim($line, "\n"), 2);
        $path = $root . '/' . $rel;
        if ($t === 'd') {
            $out->w(bk_tar_header($rel . '/', 0, @fileperms($path) ?: 0755, (int)@filemtime($path), '5'));
            $st['n']++;
            continue;
        }
        if ($t === 'l') {
            $out->w(bk_tar_header($rel, 0, 0777, (int)@filemtime($path), '2', (string)@readlink($path)));
            $st['n']++;
            continue;
        }
        if ($st['off'] === 0) {
            if (!is_readable($path) || ($size = @filesize($path)) === false) {
                $st['skip']++;
                if (count($log) < 20) {
                    $log[] = "нет доступа к файлу, пропущен: $rel";
                }
                continue;
            }
            $out->w(bk_tar_header($rel, $size, @fileperms($path) ?: 0644, (int)@filemtime($path), '0'));
            $st['sz'] = $size;
        }
        $size = $st['sz'];
        $fh = @fopen($path, 'rb');
        if ($fh && $st['off'] > 0) {
            fseek($fh, $st['off']);
        }
        while ($st['off'] < $size) {
            $chunk = $fh ? fread($fh, (int)min(1048576, $size - $st['off'])) : '';
            if ($chunk === '' || $chunk === false) {
                // The file shrank while being archived: keep the tar valid with zeros.
                $chunk = str_repeat("\0", (int)min(1048576, $size - $st['off']));
            }
            $out->w($chunk);
            $st['off'] += strlen($chunk);
            if ($st['off'] < $size && $budget->over($out)) {
                break;
            }
        }
        if ($fh) {
            fclose($fh);
        }
        if ($st['off'] < $size) {
            fseek($lf, $pos); // continue this file in the next request
            break;
        }
        if ($size % 512) {
            $out->w(str_repeat("\0", 512 - $size % 512));
        }
        $st['off'] = 0;
        $st['sz'] = 0;
        $st['n']++;
    }
    $st['lo'] = ftell($lf);
    fclose($lf);
    if ($st['ph'] === 'done') {
        @unlink(bk_list_path($root));
        $log[] = "заархивировано объектов: {$st['n']}" . ($st['skip'] ? ", пропущено без доступа: {$st['skip']}" : '');
    }
    $raw = $out->raw;
    bk_send(['state' => $st, 'done' => $st['ph'] === 'done', 'raw' => $raw, 'log' => $log], $out->finish());
}

// ---------- dispatch ----------

try {
    $p = json_decode($bkParams, true);
    if (!is_array($p)) {
        $p = [];
    }
    $root = str_replace('\\', '/', __DIR__);
    $budget = new BkBudget(max(3, (float)($p['sec'] ?? 20)), max(262144, (int)($p['max'] ?? 8388608)));
    switch ($bkAction) {
        case 'info':
            bk_info($root, $p);
            break;
        case 'db':
            bk_dump($root, $p, $budget);
            break;
        case 'files':
            bk_files($root, $p, $budget);
            break;
        case 'cleanup':
            @unlink(bk_list_path($root));
            bk_send(['done' => true]);
            break;
    }
    bk_send(['err' => 'unknown action']);
} catch (Throwable $e) {
    bk_send(['err' => $e->getMessage()]);
}
