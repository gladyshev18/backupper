import { Github, Mail, Send, ShieldCheck } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'

const author = {
  name: 'Гладышев Игорь',
  telegram: 'gladyshev',
  email: 'gladyshev18@gmail.com',
  github: 'https://github.com/gladyshev18/backupper',
}

const stack = ['Go', 'Wails', 'React', 'TypeScript', 'SSH', 'age', 'gzip', 'systemd']

export default function About() {
  const { state } = useApp()
  const open = (url: string) => (e: React.MouseEvent) => { e.preventDefault(); api.openURL(url) }

  return (
    <div className="page" style={{ maxWidth: 820 }}>
      <div className="card about-hero">
        <div className="brand-logo about-logo"><ShieldCheck size={34} /></div>
        <h1>Backupper</h1>
        <div className="muted">Бэкапы серверов в Яндекс Диск и Облако Mail.ru</div>
        <span className="badge accent mt12">версия {state.version}</span>
      </div>

      <div className="section-title">Автор</div>
      <div className="card card-pad about-author">
        <div className="about-avatar">ГИ</div>
        <div className="grow">
          <div className="about-name">{author.name}</div>
          <div className="muted small">Автор и разработчик</div>
          <div className="row wrap mt16" style={{ gap: 8 }}>
            <a className="btn" href="#" onClick={open(`https://t.me/${author.telegram}`)}><Send size={15} /> @{author.telegram}</a>
            <a className="btn" href="#" onClick={open(`mailto:${author.email}`)}><Mail size={15} /> {author.email}</a>
            <a className="btn" href="#" onClick={open(author.github)}><Github size={15} /> GitHub</a>
          </div>
        </div>
      </div>

      <div className="section-title">О программе</div>
      <div className="card card-pad">
        <div className="muted">
          Агент на сервере сам делает бэкапы по расписанию и потоком выгружает их в облако кусками, не занимая место на диске.
          Панель управляет агентами через SSH-туннель по вашему ключу.
        </div>
        <div className="row wrap mt16" style={{ gap: 6 }}>
          {stack.map(s => <span key={s} className="chip">{s}</span>)}
        </div>
        <div className="small faint mt16">Вопросы, идеи и сообщения об ошибках — в Telegram или на почту.</div>
      </div>
    </div>
  )
}
