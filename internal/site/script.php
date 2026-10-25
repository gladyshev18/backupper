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

function bk_cms($root)
{
    if (is_file("$root/bitrix/.settings.php") || is_file("$root/bitrix/php_interface/dbconn.php")) {
        return 'bitrix';
    }
    if (is_file("$root/wp-config.php") || is_file(dirname($root) . '/wp-config.php')) {
        return 'wordpress';
    }
    return '';
}

function bk_php_string($src, $name)
{
    $re = "/define\\s*\\(\\s*['\"]" . preg_quote($name, '/') . "['\"]\\s*,\\s*(?:'((?:[^'\\\\]|\\\\.)*)'|\"((?:[^\"\\\\]|\\\\.)*)\")/s";
    if (!preg_match($re, $src, $m)) {
        return null;
    }
    if (isset($m[2]) && $m[2] !== '') {
        return stripcslashes($m[2]);
    }
    return str_replace(["\\'", '\\\\'], ["'", '\\'], $m[1]);
}

function bk_db_settings($root, $cms)
{
    if ($cms === 'bitrix') {
        $f = "$root/bitrix/.settings.php";
        if (is_file($f)) {
            $s = include $f;
            $c = $s['connections']['value']['default'] ?? null;
            if (is_array($c) && !empty($c['database'])) {
                return ['host' => (string)$c['host'], 'name' => (string)$c['database'], 'user' => (string)$c['login'], 'pass' => (string)$c['password'], 'from' => 'bitrix/.settings.php'];
            }
        }
        $f = "$root/bitrix/php_interface/dbconn.php";
        if (is_file($f)) {
            $r = (function ($f) {
                $DBHost = $DBLogin = $DBPassword = $DBName = null;
                @include $f;
                return [$DBHost, $DBName, $DBLogin, $DBPassword];
            })($f);
            if (!empty($r[1])) {
                return ['host' => (string)$r[0], 'name' => (string)$r[1], 'user' => (string)$r[2], 'pass' => (string)$r[3], 'from' => 'bitrix/php_interface/dbconn.php'];
            }
        }
    }
    if ($cms === 'wordpress') {
        $f = is_file("$root/wp-config.php") ? "$root/wp-config.php" : dirname($root) . '/wp-config.php';
        $src = (string)@file_get_contents($f);
        $name = bk_php_string($src, 'DB_NAME');
        if ($name !== null) {
            return ['host' => (string)bk_php_string($src, 'DB_HOST'), 'name' => $name, 'user' => (string)bk_php_string($src, 'DB_USER'), 'pass' => (string)bk_php_string($src, 'DB_PASSWORD'), 'from' => 'wp-config.php'];
        }
    }
    return null;
}

function bk_db_resolve($root, array $p)
{
    $o = $p['db'] ?? [];
    if (!empty($o['name'])) {
        return ['host' => (string)($o['host'] ?? 'localhost'), 'name' => (string)$o['name'], 'user' => (string)($o['user'] ?? ''), 'pass' => (string)($o['pass'] ?? ''), 'from' => 'панель'];
    }
    $cms = ($p['cms'] ?? 'auto') === 'auto' ? bk_cms($root) : (string)$p['cms'];
    return bk_db_settings($root, $cms);
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
    $opts = [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION, PDO::ATTR_EMULATE_PREPARES => true];
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
    $db = bk_db_resolve($root, $p);
    if ($db) {
        $res['db'] = ['host' => $db['host'], 'name' => $db['name'], 'user' => $db['user'], 'from' => $db['from']];
        try {
            list($pdo) = bk_pdo($db);
            $st = $pdo->prepare('SELECT COUNT(*), COALESCE(SUM(data_length + index_length), 0) FROM information_schema.TABLES WHERE table_schema = ?');
            $st->execute([$db['name']]);
            $r = $st->fetch(PDO::FETCH_NUM);
            $res['db']['tables'] = (int)$r[0];
            $res['db']['size'] = (float)$r[1];
            $res['db']['server'] = $pdo->getAttribute(PDO::ATTR_SERVER_VERSION);
        } catch (Exception $e) {
            $res['db']['error'] = $e->getMessage();
        }
    }
    bk_send(['info' => $res, 'done' => true]);
}

function bk_dump($root, array $p, BkBudget $budget)
{
    $db = bk_db_resolve($root, $p);
    if (!$db) {
        bk_send(['err' => 'не найдены настройки базы данных (bitrix/.settings.php, wp-config.php) — укажите их вручную']);
    }
    list($pdo, $cs) = bk_pdo($db);
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
    }
    fclose($f);
    return [$count, $skip];
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
        list($count, $skip) = bk_build_list($root, $pats, $anchored);
        $log[] = "в архив попадёт объектов: $count, исключено по шаблонам: $skip";
        $st = ['ph' => 'tar', 'lo' => 0, 'off' => 0, 'sz' => 0, 'n' => 0, 'skip' => 0];
        bk_send(['state' => $st, 'done' => false, 'raw' => 0, 'log' => $log]);
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
