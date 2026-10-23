import type { Job, Schedule, Source, StorageKind } from './types'
import { locale, t } from './i18n'

export function bytes(n?: number): string {
  const u = [t('Б'), t('КБ'), t('МБ'), t('ГБ'), t('ТБ')]
  if (!n || n <= 0) return `0 ${u[0]}`
  let i = 0
  let v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v >= 100 || i === 0 || Number.isInteger(v) ? v.toFixed(0) : v.toFixed(1)} ${u[i]}`
}

export function speed(bps?: number): string {
  return bps && bps > 0 ? `${bytes(bps)}/${t('с')}` : '—'
}

const isZero = (s?: string) => !s || s.startsWith('0001-')

export function dateTime(s?: string): string {
  if (isZero(s)) return '—'
  const d = new Date(s!)
  const now = new Date()
  const time = d.toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' })
  const y = new Date(now); y.setDate(now.getDate() - 1)
  const tm = new Date(now); tm.setDate(now.getDate() + 1)
  if (d.toDateString() === now.toDateString()) return t('сегодня, {time}', { time })
  if (d.toDateString() === y.toDateString()) return t('вчера, {time}', { time })
  if (d.toDateString() === tm.toDateString()) return t('завтра, {time}', { time })
  return `${d.toLocaleDateString(locale(), { day: 'numeric', month: 'short' })}, ${time}`
}

export function relative(s?: string): string {
  if (isZero(s)) return '—'
  const diff = (new Date(s!).getTime() - Date.now()) / 1000
  const a = Math.abs(diff)
  let v: string
  if (a < 60) v = t('меньше минуты')
  else if (a < 3600) v = t('{n} мин', { n: Math.round(a / 60) })
  else if (a < 86400) v = t('{n} ч', { n: Math.round(a / 3600) })
  else v = t('{n} дн', { n: Math.round(a / 86400) })
  return diff > 0 ? t('через {v}', { v }) : t('{v} назад', { v })
}

export function duration(from?: string, to?: string): string {
  if (isZero(from)) return '—'
  const end = isZero(to) ? Date.now() : new Date(to!).getTime()
  const s = Math.max(0, Math.round((end - new Date(from!).getTime()) / 1000))
  if (s < 60) return t('{s} с', { s })
  if (s < 3600) return t('{m} мин {s} с', { m: Math.floor(s / 60), s: s % 60 })
  return t('{h} ч {m} мин', { h: Math.floor(s / 3600), m: Math.floor((s % 3600) / 60) })
}

export function seconds(n: number): string {
  return n < 90 ? t('{s} с', { s: Math.max(1, Math.round(n)) }) : t('{m} мин', { m: (n / 60).toFixed(1) })
}

const dayNames = ['вс', 'пн', 'вт', 'ср', 'чт', 'пт', 'сб']

export function scheduleText(s: Schedule): string {
  if (!s.enabled) return t('Только вручную')
  const time = s.time || '03:00'
  switch (s.mode) {
    case 'daily': return t('Каждый день в {time}', { time })
    case 'weekly': {
      const d = [...(s.weekdays || [])].sort((a, b) => ((a + 6) % 7) - ((b + 6) % 7)).map(i => t(dayNames[i]))
      return t('По {days} в {time}', { days: d.join(', '), time })
    }
    case 'monthly': return t('{d}-го числа в {time}', { d: s.monthDay || 1, time })
    case 'interval': return t('Каждые {n} ч', { n: s.everyHours })
    case 'cron': return `cron: ${s.cron}`
  }
  return ''
}

// Labels are kept in Russian and translated where shown: t(sourceLabels[x]).
export const sourceLabels: Record<Source['type'], string> = {
  mysql: 'MySQL / MariaDB',
  postgres: 'PostgreSQL',
  files: 'Файлы сайта',
  configs: 'Конфиги сервера',
  site: 'Сайт на хостинге',
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
    case 'site': return { id: uid(), type, name: 'site', excludes: [], site: { protocol: 'ftp', host: '', port: 0, user: '', password: '', root: '/public_html', url: 'https://', db: true, files: true, cms: 'auto', cmsExcludes: true } }
  }
}

export function cls(...a: (string | false | undefined | null)[]): string {
  return a.filter(Boolean).join(' ')
}
