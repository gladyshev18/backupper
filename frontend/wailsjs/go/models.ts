export namespace cms {
	
	export class Preset {
	    id: string;
	    name: string;
	    markers: string[];
	    excludes: string[];
	
	    static createFrom(source: any = {}) {
	        return new Preset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.markers = source["markers"];
	        this.excludes = source["excludes"];
	    }
	}

}

export namespace desktop {
	
	export class Server {
	    id: string;
	    name: string;
	    host: string;
	    port: number;
	    user: string;
	    keyPath: string;
	    passphrase?: string;
	    agentPort: number;
	    token?: string;
	    hostKey?: string;
	
	    static createFrom(source: any = {}) {
	        return new Server(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.keyPath = source["keyPath"];
	        this.passphrase = source["passphrase"];
	        this.agentPort = source["agentPort"];
	        this.token = source["token"];
	        this.hostKey = source["hostKey"];
	    }
	}
	export class ServerCheck {
	    ok: boolean;
	    error?: string;
	    os: string;
	    arch: string;
	    root: boolean;
	    sudo: boolean;
	    systemd: boolean;
	    agentInstalled: boolean;
	    agentVersion: string;
	    fingerprint: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.root = source["root"];
	        this.sudo = source["sudo"];
	        this.systemd = source["systemd"];
	        this.agentInstalled = source["agentInstalled"];
	        this.agentVersion = source["agentVersion"];
	        this.fingerprint = source["fingerprint"];
	    }
	}
	export class Settings {
	    encryptionPassword: string;
	    telegram: model.Telegram;
	    theme: string;
	    yandexClientId: string;
	    language: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.encryptionPassword = source["encryptionPassword"];
	        this.telegram = this.convertValues(source["telegram"], model.Telegram);
	        this.theme = source["theme"];
	        this.yandexClientId = source["yandexClientId"];
	        this.language = source["language"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class RetentionPreview {
	    ages: number[];
	    reasons: number[];
	    assumed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RetentionPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ages = source["ages"];
	        this.reasons = source["reasons"];
	        this.assumed = source["assumed"];
	    }
	}
	export class ServerStatus {
	    id: string;
	    online: boolean;
	    error?: string;
	    noAgent: boolean;
	    info?: model.AgentInfo;
	    active: model.Run[];
	    recent: model.Run[];
	    outdated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.online = source["online"];
	        this.error = source["error"];
	        this.noAgent = source["noAgent"];
	        this.info = this.convertValues(source["info"], model.AgentInfo);
	        this.active = this.convertValues(source["active"], model.Run);
	        this.recent = this.convertValues(source["recent"], model.Run);
	        this.outdated = source["outdated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServerView {
	    id: string;
	    name: string;
	    host: string;
	    port: number;
	    user: string;
	    keyPath: string;
	    passphrase?: string;
	    agentPort: number;
	    token?: string;
	    hostKey?: string;
	    hasToken: boolean;
	    fingerprint: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.keyPath = source["keyPath"];
	        this.passphrase = source["passphrase"];
	        this.agentPort = source["agentPort"];
	        this.token = source["token"];
	        this.hostKey = source["hostKey"];
	        this.hasToken = source["hasToken"];
	        this.fingerprint = source["fingerprint"];
	    }
	}
	export class State {
	    version: string;
	    servers: ServerView[];
	    storages: model.Storage[];
	    settings: desktop.Settings;
	    keys: string[];
	    kinds: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.servers = this.convertValues(source["servers"], ServerView);
	        this.storages = this.convertValues(source["storages"], model.Storage);
	        this.settings = this.convertValues(source["settings"], desktop.Settings);
	        this.keys = source["keys"];
	        this.kinds = source["kinds"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace model {
	
	export class Progress {
	    stage: string;
	    source: string;
	    rawBytes: number;
	    bytes: number;
	    part: number;
	    speedBps: number;
	
	    static createFrom(source: any = {}) {
	        return new Progress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stage = source["stage"];
	        this.source = source["source"];
	        this.rawBytes = source["rawBytes"];
	        this.bytes = source["bytes"];
	        this.part = source["part"];
	        this.speedBps = source["speedBps"];
	    }
	}
	export class LogLine {
	    // Go type: time
	    time: any;
	    level: string;
	    msg: string;
	
	    static createFrom(source: any = {}) {
	        return new LogLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = this.convertValues(source["time"], null);
	        this.level = source["level"];
	        this.msg = source["msg"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UploadStat {
	    bytes: number;
	    seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new UploadStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bytes = source["bytes"];
	        this.seconds = source["seconds"];
	    }
	}
	export class Part {
	    name: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new Part(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	    }
	}
	export class RunFile {
	    name: string;
	    source: string;
	    size: number;
	    rawSize: number;
	    sha256: string;
	    encrypted: boolean;
	    parts: Part[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.source = source["source"];
	        this.size = source["size"];
	        this.rawSize = source["rawSize"];
	        this.sha256 = source["sha256"];
	        this.encrypted = source["encrypted"];
	        this.parts = this.convertValues(source["parts"], Part);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Run {
	    id: string;
	    jobId: string;
	    jobName: string;
	    trigger: string;
	    status: string;
	    // Go type: time
	    queuedAt: any;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    finishedAt: any;
	    remoteDir: string;
	    storages: string[];
	    files: RunFile[];
	    bytes: number;
	    rawBytes: number;
	    chunkSize: number;
	    chunkBuffer: string;
	    uploads?: Record<string, UploadStat>;
	    error?: string;
	    log?: LogLine[];
	    progress?: Progress;
	
	    static createFrom(source: any = {}) {
	        return new Run(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.jobId = source["jobId"];
	        this.jobName = source["jobName"];
	        this.trigger = source["trigger"];
	        this.status = source["status"];
	        this.queuedAt = this.convertValues(source["queuedAt"], null);
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	        this.remoteDir = source["remoteDir"];
	        this.storages = source["storages"];
	        this.files = this.convertValues(source["files"], RunFile);
	        this.bytes = source["bytes"];
	        this.rawBytes = source["rawBytes"];
	        this.chunkSize = source["chunkSize"];
	        this.chunkBuffer = source["chunkBuffer"];
	        this.uploads = this.convertValues(source["uploads"], UploadStat, true);
	        this.error = source["error"];
	        this.log = this.convertValues(source["log"], LogLine);
	        this.progress = this.convertValues(source["progress"], Progress);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChunkSettings {
	    mode: string;
	    sizeMB: number;
	    maxMemoryPercent: number;
	    targetSeconds: number;
	    buffer: string;
	
	    static createFrom(source: any = {}) {
	        return new ChunkSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.sizeMB = source["sizeMB"];
	        this.maxMemoryPercent = source["maxMemoryPercent"];
	        this.targetSeconds = source["targetSeconds"];
	        this.buffer = source["buffer"];
	    }
	}
	export class Retention {
	    last: number;
	    daily: number;
	    weekly: number;
	    monthly: number;
	    yearly: number;
	
	    static createFrom(source: any = {}) {
	        return new Retention(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.last = source["last"];
	        this.daily = source["daily"];
	        this.weekly = source["weekly"];
	        this.monthly = source["monthly"];
	        this.yearly = source["yearly"];
	    }
	}
	export class Schedule {
	    enabled: boolean;
	    mode: string;
	    time: string;
	    weekdays: number[];
	    monthDay: number;
	    everyHours: number;
	    cron: string;
	
	    static createFrom(source: any = {}) {
	        return new Schedule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.mode = source["mode"];
	        this.time = source["time"];
	        this.weekdays = source["weekdays"];
	        this.monthDay = source["monthDay"];
	        this.everyHours = source["everyHours"];
	        this.cron = source["cron"];
	    }
	}
	export class SiteSource {
	    protocol: string;
	    host: string;
	    port?: number;
	    user: string;
	    password?: string;
	    root: string;
	    url: string;
	    db: boolean;
	    files: boolean;
	    cms: string;
	    cmsExcludes: boolean;
	    dbHost?: string;
	    dbName?: string;
	    dbUser?: string;
	    dbPassword?: string;
	    insecure?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SiteSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocol = source["protocol"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.password = source["password"];
	        this.root = source["root"];
	        this.url = source["url"];
	        this.db = source["db"];
	        this.files = source["files"];
	        this.cms = source["cms"];
	        this.cmsExcludes = source["cmsExcludes"];
	        this.dbHost = source["dbHost"];
	        this.dbName = source["dbName"];
	        this.dbUser = source["dbUser"];
	        this.dbPassword = source["dbPassword"];
	        this.insecure = source["insecure"];
	    }
	}
	export class Source {
	    id: string;
	    type: string;
	    name: string;
	    host?: string;
	    port?: number;
	    user?: string;
	    password?: string;
	    databases?: string[];
	    perDatabase?: boolean;
	    extraArgs?: string;
	    paths?: string[];
	    excludes?: string[];
	    cmsExcludes?: boolean;
	    site?: SiteSource;
	
	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.password = source["password"];
	        this.databases = source["databases"];
	        this.perDatabase = source["perDatabase"];
	        this.extraArgs = source["extraArgs"];
	        this.paths = source["paths"];
	        this.excludes = source["excludes"];
	        this.cmsExcludes = source["cmsExcludes"];
	        this.site = this.convertValues(source["site"], SiteSource);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobView {
	    id: string;
	    name: string;
	    sources: Source[];
	    schedule: Schedule;
	    storages: string[];
	    keep: number;
	    retention: Retention;
	    encrypt: boolean;
	    compression: number;
	    chunk: ChunkSettings;
	    notify: string;
	    // Go type: time
	    nextRun?: any;
	    lastRun?: Run;
	    lastSize?: number;
	    cronError?: string;
	
	    static createFrom(source: any = {}) {
	        return new JobView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.sources = this.convertValues(source["sources"], Source);
	        this.schedule = this.convertValues(source["schedule"], Schedule);
	        this.storages = source["storages"];
	        this.keep = source["keep"];
	        this.retention = this.convertValues(source["retention"], Retention);
	        this.encrypt = source["encrypt"];
	        this.compression = source["compression"];
	        this.chunk = this.convertValues(source["chunk"], ChunkSettings);
	        this.notify = source["notify"];
	        this.nextRun = this.convertValues(source["nextRun"], null);
	        this.lastRun = this.convertValues(source["lastRun"], Run);
	        this.lastSize = source["lastSize"];
	        this.cronError = source["cronError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Storage {
	    id: string;
	    name: string;
	    kind: string;
	    token?: string;
	    login?: string;
	    password?: string;
	    url?: string;
	    root: string;
	
	    static createFrom(source: any = {}) {
	        return new Storage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.token = source["token"];
	        this.login = source["login"];
	        this.password = source["password"];
	        this.url = source["url"];
	        this.root = source["root"];
	    }
	}
	export class Telegram {
	    enabled: boolean;
	    botToken: string;
	    chatId: string;
	
	    static createFrom(source: any = {}) {
	        return new Telegram(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.botToken = source["botToken"];
	        this.chatId = source["chatId"];
	    }
	}
	export class Settings {
	    serverName: string;
	    encryptionPassword: string;
	    telegram: Telegram;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverName = source["serverName"];
	        this.encryptionPassword = source["encryptionPassword"];
	        this.telegram = this.convertValues(source["telegram"], Telegram);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SysInfo {
	    hostname: string;
	    os: string;
	    kernel: string;
	    arch: string;
	    cpus: number;
	    load1: number;
	    memTotal: number;
	    memAvailable: number;
	    swapTotal: number;
	    swapFree: number;
	    tmpDir: string;
	    diskTotal: number;
	    diskFree: number;
	    uptime: number;
	    tz: string;
	    tzOffset: number;
	    tools: Record<string, boolean>;
	
	    static createFrom(source: any = {}) {
	        return new SysInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hostname = source["hostname"];
	        this.os = source["os"];
	        this.kernel = source["kernel"];
	        this.arch = source["arch"];
	        this.cpus = source["cpus"];
	        this.load1 = source["load1"];
	        this.memTotal = source["memTotal"];
	        this.memAvailable = source["memAvailable"];
	        this.swapTotal = source["swapTotal"];
	        this.swapFree = source["swapFree"];
	        this.tmpDir = source["tmpDir"];
	        this.diskTotal = source["diskTotal"];
	        this.diskFree = source["diskFree"];
	        this.uptime = source["uptime"];
	        this.tz = source["tz"];
	        this.tzOffset = source["tzOffset"];
	        this.tools = source["tools"];
	    }
	}
	export class AgentInfo {
	    version: string;
	    sys: SysInfo;
	    settings: Settings;
	    storages: Storage[];
	    jobs: JobView[];
	
	    static createFrom(source: any = {}) {
	        return new AgentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.sys = this.convertValues(source["sys"], SysInfo);
	        this.settings = this.convertValues(source["settings"], Settings);
	        this.storages = this.convertValues(source["storages"], Storage);
	        this.jobs = this.convertValues(source["jobs"], JobView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BackupEntry {
	    dir: string;
	    name: string;
	    status: string;
	    // Go type: time
	    started: any;
	    size: number;
	    files: RunFile[];
	    complete: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackupEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.name = source["name"];
	        this.status = source["status"];
	        this.started = this.convertValues(source["started"], null);
	        this.size = source["size"];
	        this.files = this.convertValues(source["files"], RunFile);
	        this.complete = source["complete"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CMSSite {
	    path: string;
	    cms: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CMSSite(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.cms = source["cms"];
	        this.name = source["name"];
	    }
	}
	export class ChunkPlan {
	    size: number;
	    buffer: string;
	    peakMemory: number;
	    peakDisk: number;
	    memAvailable: number;
	    diskFree: number;
	    speedBps: number;
	    expectedSize: number;
	    parts: number;
	    partSeconds: number;
	    maxFileSize: number;
	    reasons: string[];
	    warnings: string[];
	    speedMeasured: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChunkPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.size = source["size"];
	        this.buffer = source["buffer"];
	        this.peakMemory = source["peakMemory"];
	        this.peakDisk = source["peakDisk"];
	        this.memAvailable = source["memAvailable"];
	        this.diskFree = source["diskFree"];
	        this.speedBps = source["speedBps"];
	        this.expectedSize = source["expectedSize"];
	        this.parts = source["parts"];
	        this.partSeconds = source["partSeconds"];
	        this.maxFileSize = source["maxFileSize"];
	        this.reasons = source["reasons"];
	        this.warnings = source["warnings"];
	        this.speedMeasured = source["speedMeasured"];
	    }
	}
	
	export class Job {
	    id: string;
	    name: string;
	    sources: Source[];
	    schedule: Schedule;
	    storages: string[];
	    keep: number;
	    retention: Retention;
	    encrypt: boolean;
	    compression: number;
	    chunk: ChunkSettings;
	    notify: string;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.sources = this.convertValues(source["sources"], Source);
	        this.schedule = this.convertValues(source["schedule"], Schedule);
	        this.storages = source["storages"];
	        this.keep = source["keep"];
	        this.retention = this.convertValues(source["retention"], Retention);
	        this.encrypt = source["encrypt"];
	        this.compression = source["compression"];
	        this.chunk = this.convertValues(source["chunk"], ChunkSettings);
	        this.notify = source["notify"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class SiteDB {
	    host: string;
	    name: string;
	    user: string;
	    from: string;
	    tables: number;
	    size: number;
	    server: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SiteDB(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.name = source["name"];
	        this.user = source["user"];
	        this.from = source["from"];
	        this.tables = source["tables"];
	        this.size = source["size"];
	        this.server = source["server"];
	        this.error = source["error"];
	    }
	}
	export class SiteInfo {
	    php: string;
	    sapi: string;
	    os: string;
	    root: string;
	    cms: string;
	    timeLimit: number;
	    memoryLimit: number;
	    diskFree: number;
	    deflate: boolean;
	    pdoMysql: boolean;
	    serverTime: number;
	    top: string[];
	    db?: SiteDB;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SiteInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.php = source["php"];
	        this.sapi = source["sapi"];
	        this.os = source["os"];
	        this.root = source["root"];
	        this.cms = source["cms"];
	        this.timeLimit = source["timeLimit"];
	        this.memoryLimit = source["memoryLimit"];
	        this.diskFree = source["diskFree"];
	        this.deflate = source["deflate"];
	        this.pdoMysql = source["pdoMysql"];
	        this.serverTime = source["serverTime"];
	        this.top = source["top"];
	        this.db = this.convertValues(source["db"], SiteDB);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProbeResult {
	    ok: boolean;
	    error?: string;
	    databases?: string[];
	    paths?: string[];
	    cms?: CMSSite[];
	    site?: SiteInfo;
	
	    static createFrom(source: any = {}) {
	        return new ProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.databases = source["databases"];
	        this.paths = source["paths"];
	        this.cms = this.convertValues(source["cms"], CMSSite);
	        this.site = this.convertValues(source["site"], SiteInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	
	
	
	
	
	export class StorageTest {
	    ok: boolean;
	    error?: string;
	    speedBps: number;
	    total: number;
	    used: number;
	    maxFile: number;
	    elapsedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new StorageTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.speedBps = source["speedBps"];
	        this.total = source["total"];
	        this.used = source["used"];
	        this.maxFile = source["maxFile"];
	        this.elapsedMs = source["elapsedMs"];
	    }
	}
	
	

}

