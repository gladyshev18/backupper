import { ArrowUpCircle, CalendarClock, Cloud, Github, Globe, Lock, Mail, MemoryStick, Send, ShieldCheck } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'
import { t } from '../i18n'

const author = {
  name: 'Гладышев Игорь',
  telegram: 'gladyshev',
  email: 'gladyshev18@gmail.com',
  github: 'https://github.com/gladyshev18/backupper',
}

const stack = ['Go', 'Wails', 'React', 'TypeScript', 'SSH', 'age', 'gzip', 'systemd', 'PHP', 'Telegram Bot API']

const features = [
  { icon: MemoryStick, text: 'Потоковая выгрузка частями из памяти — свободное место на диске сервера не нужно' },
  { icon: Cloud, text: 'Яндекс Диск, Облако Mail.ru, любой WebDAV и Telegram через свой сервер Bot API' },
  { icon: Globe, text: 'Сайты на обычном хостинге по FTP, FTPS или SFTP — доступ к базе берётся из настроек 13 CMS' },
  { icon: CalendarClock, text: 'Расписание и ротация по календарю: последние копии, по дням, неделям, месяцам и годам' },
  { icon: Lock, text: 'Шифрование age, SSH-туннель к агенту, уведомления в Telegram прямо с сервера' },
  { icon: ArrowUpCircle, text: 'Установка и обновление агента одной кнопкой — на одном сервере или сразу на всех' },
]

export default function About() {
  const { state } = useApp()
  const open = (url: string) => (e: React.MouseEvent) => { e.preventDefault(); api.openURL(url) }

  return (
    <div className="page" style={{ maxWidth: 820 }}>
      <div className="card about-hero">
        <div className="brand-logo about-logo"><ShieldCheck size={34} /></div>
        <h1>Backupper</h1>
        <div className="muted">{t('Бэкапы серверов и сайтов на хостинге в облака и Telegram')}</div>
        <span className="badge accent mt12">{t('версия {v}', { v: state.version })}</span>
      </div>

      <div className="section-title">{t('Автор')}</div>
      <div className="card card-pad about-author">
        <div className="about-avatar">{t('ГИ')}</div>
        <div className="grow">
          <div className="about-name">{t(author.name)}</div>
          <div className="muted small">{t('Автор и разработчик')}</div>
          <div className="row wrap mt16" style={{ gap: 8 }}>
            <a className="btn" href="#" onClick={open(`https://t.me/${author.telegram}`)}><Send size={15} /> @{author.telegram}</a>
            <a className="btn" href="#" onClick={open(`mailto:${author.email}`)}><Mail size={15} /> {author.email}</a>
            <a className="btn" href="#" onClick={open(author.github)}><Github size={15} /> GitHub</a>
          </div>
        </div>
      </div>

      <div className="section-title">{t('О программе')}</div>
      <div className="card card-pad">
        <div className="muted">
          {t('Агент на сервере сам делает бэкапы по расписанию и потоком выгружает их в облако кусками, не занимая место на диске. Панель управляет агентами через SSH-туннель по вашему ключу.')}
        </div>
        <div className="grid g2 mt16" style={{ gap: 10 }}>
          {features.map(({ icon: Icon, text }) => (
            <div key={text} className="row small" style={{ alignItems: 'flex-start', gap: 8 }}>
              <Icon size={15} className="faint" style={{ flexShrink: 0, marginTop: 2 }} /><span>{t(text)}</span>
            </div>
          ))}
        </div>
        <div className="row wrap mt16" style={{ gap: 6 }}>
          {stack.map(s => <span key={s} className="chip">{s}</span>)}
        </div>
        <div className="small faint mt16">{t('Вопросы, идеи и сообщения об ошибках — в Telegram или на почту.')}</div>
      </div>
    </div>
  )
}
