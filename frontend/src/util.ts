import type { Job, Schedule, Source, StorageKind } from './types'

export function bytes(n?: number): string {
  if (!n || n <= 0) return '0 Б'
  const u = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ']
  let i = 0
  let v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v >= 100 || i === 0 || Number.isInteger(v) ? v.toFixed(0) : v.toFixed(1)} ${u[i]}`
}

export function speed(bps?: number): string {
  return bps && bps > 0 ? `${bytes(bps)}/с` : '—'
}

const isZero = (t?: string) => !t || t.startsWith('0001-')

export function dateTime(t?: string): string {
  if (isZero(t)) return '—'
  const d = new Date(t!)
  const now = new Date()
  const time = d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  const sameDay = d.toDateString() === now.toDateString()
  const y = new Date(now); y.setDate(now.getDate() - 1)
  const tm = new Date(now); tm.setDate(now.getDate() + 1)
  if (sameDay) return `сегодня, ${time}`
  if (d.toDateString() === y.toDateString()) return `вчера, ${time}`
  if (d.toDateString() === tm.toDateString()) return `завтра, ${time}`
  return `${d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' })}, ${time}`
}

export function relative(t?: string): string {
  if (isZero(t)) return '—'
  const diff = (new Date(t!).getTime() - Date.now()) / 1000
  const a = Math.abs(diff)
  let s: string
  if (a < 60) s = 'меньше минуты'
  else if (a < 3600) s = `${Math.round(a / 60)} мин`
  else if (a < 86400) s = `${Math.round(a / 3600)} ч`
  else s = `${Math.round(a / 86400)} дн`
  return diff > 0 ? `через ${s}` : `${s} назад`
}

export function duration(from?: string, to?: string): string {
  if (isZero(from)) return '—'
  const end = isZero(to) ? Date.now() : new Date(to!).getTime()
  const s = Math.max(0, Math.round((end - new Date(from!).getTime()) / 1000))
  if (s < 60) return `${s} с`
  if (s < 3600) return `${Math.floor(s / 60)} мин ${s % 60} с`
  return `${Math.floor(s / 3600)} ч ${Math.floor((s % 3600) / 60)} мин`
}

const dayNames = ['вс', 'пн', 'вт', 'ср', 'чт', 'пт', 'сб']

export function scheduleText(s: Schedule): string {
  if (!s.enabled) return 'Только вручную'
  const t = s.time || '03:00'
  switch (s.mode) {
    case 'daily': return `Каждый день в ${t}`
    case 'weekly': {
      const d = [...(s.weekdays || [])].sort((a, b) => ((a + 6) % 7) - ((b + 6) % 7)).map(i => dayNames[i])
      return `По ${d.join(', ')} в ${t}`
    }
    case 'monthly': return `${s.monthDay || 1}-го числа в ${t}`
    case 'interval': return `Каждые ${s.everyHours} ч`
    case 'cron': return `cron: ${s.cron}`
  }
  return ''
}

export const sourceLabels: Record<Source['type'], string> = {
  mysql: 'MySQL / MariaDB',
  postgres: 'PostgreSQL',
  files: 'Файлы сайта',
  configs: 'Конфиги сервера',
}

export const kindLabels: Record<StorageKind, string> = {
  yandex: 'Яндекс Диск',
  yandex_webdav: 'Яндекс Диск (WebDAV)',
  mailru: 'Облако Mail.ru',
  webdav: 'WebDAV',
}

export const statusLabels: Record<string, string> = {
  queued: 'В очереди', running: 'Выполняется', success: 'Успешно', warning: 'С предупреждениями',
  failed: 'Ошибка', canceled: 'Отменено', incomplete: 'Не завершена',
}

export const configPresets = [
  '/etc/nginx', '/etc/apache2', '/etc/httpd', '/etc/php', '/etc/php-fpm.d', '/etc/mysql', '/etc/my.cnf',
  '/etc/my.cnf.d', '/etc/postgresql', '/etc/redis', '/etc/letsencrypt', '/etc/ssh/sshd_config', '/etc/crontab',
  '/etc/cron.d', '/var/spool/cron', '/etc/systemd/system', '/etc/supervisor', '/etc/fail2ban', '/etc/hosts',
  '/etc/fstab', '/etc/sysctl.conf', '/etc/ufw', '/etc/iptables',
]

export function uid(): string {
  return Math.random().toString(16).slice(2, 10)
}

export function newJob(storages: string[]): Job {
  return {
    id: '',
    name: '',
    sources: [],
    schedule: { enabled: true, mode: 'daily', time: '03:00', weekdays: [1, 3, 5], monthDay: 1, everyHours: 6, cron: '0 3 * * *' },
    storages,
    keep: 7,
    encrypt: false,
    compression: 6,
    chunk: { mode: 'auto', sizeMB: 64, maxMemoryPercent: 20, targetSeconds: 60, buffer: 'auto' },
    notify: 'errors',
  }
}

export function newSource(type: Source['type']): Source {
  switch (type) {
    case 'mysql': return { id: uid(), type, name: 'mysql', host: '', port: 0, user: '', password: '', databases: [], perDatabase: true }
    case 'postgres': return { id: uid(), type, name: 'postgres', host: '', port: 0, user: '', password: '', databases: [], perDatabase: true }
    case 'files': return { id: uid(), type, name: 'site', paths: ['/var/www/'], excludes: ['*/cache/*', '*.log', 'node_modules', '.git'] }
    case 'configs': return { id: uid(), type, name: 'configs', paths: [...configPresets], excludes: [] }
  }
}

export function cls(...a: (string | false | undefined | null)[]): string {
  return a.filter(Boolean).join(' ')
}
