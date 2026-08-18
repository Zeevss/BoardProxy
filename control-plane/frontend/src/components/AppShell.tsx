import type { Language, NodeSummary, Section } from '../types'
import { t } from '../i18n'
import { Icon } from './Icon'

const groups: Array<{ index: number; sections: Section[] }> = [
  { index: 0, sections: ['overview', 'nodes'] },
  { index: 1, sections: ['users', 'boards'] },
  { index: 2, sections: ['traffic', 'activity'] },
  { index: 3, sections: ['settings'] },
]

export function AppShell({ username, language, section, nodes, search, streamConnected, children, onLanguage, onSection, onSearch, onRefresh, onLogout }: {
  username: string; language: Language; section: Section; nodes: NodeSummary[]; search: string; streamConnected: boolean; children: React.ReactNode
  onLanguage: (language: Language) => void; onSection: (section: Section) => void; onSearch: (query: string) => void; onRefresh: () => void; onLogout: () => void
}) {
  const copy = t(language)
  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><span className="brand-mark"><span/></span><div><strong>BoardProxy</strong><small>{copy.brandSub}</small></div></div>
      <nav aria-label="Primary navigation">
        {groups.map(group => <div className="nav-group" key={group.index}><span className="nav-label">{copy.groups[group.index]}</span>{group.sections.map(item => <button type="button" key={item} className={section === item ? 'nav-row active' : 'nav-row'} onClick={() => onSection(item)}><Icon name={item}/><span>{copy.nav[item]}</span>{item === 'nodes' && nodes.length ? <em>{nodes.length}</em> : null}</button>)}</div>)}
      </nav>
      <div className="profile-card"><span>{username.slice(0, 2).toUpperCase()}</span><div><strong>{username}</strong><small>{copy.soleAdmin}</small></div><button type="button" title={copy.logout} onClick={onLogout}><Icon name="logout" size={15}/></button></div>
      <div className="language-toggle"><button className={language === 'en' ? 'active' : ''} onClick={() => onLanguage('en')}>EN</button><button className={language === 'ru' ? 'active' : ''} onClick={() => onLanguage('ru')}>RU</button></div>
    </aside>
    <div className="workspace">
      <header className="topbar">
        <div className="mobile-brand"><span className="brand-mark"><span/></span><strong>BoardProxy</strong></div>
        <label className="search"><Icon name="search" size={16}/><input value={search} onChange={event => onSearch(event.target.value)} placeholder={copy.search}/></label>
        <div className="topbar-spacer"/>
        <span className="fleet-scope"><span className="dot ok"/>{copy.allNodes} · {nodes.length}</span>
        <span className={streamConnected ? 'live-badge live' : 'live-badge down'}><span className="dot"/>{streamConnected ? copy.live : copy.reconnecting}</span>
        <button type="button" className="icon-button bordered" aria-label={copy.refresh} onClick={onRefresh}><Icon name="refresh" size={16}/></button>
      </header>
      <div className="mobile-nav">{Object.entries(copy.nav).map(([id, label]) => <button type="button" key={id} className={section === id ? 'active' : ''} onClick={() => onSection(id as Section)}>{label}</button>)}</div>
      <main>{children}</main>
    </div>
  </div>
}

export function PageHeader({ language, section, action }: { language: Language; section: Section; action?: React.ReactNode }) {
  const [title, subtitle] = t(language).titles[section]
  return <div className="page-header"><div><h1>{title}</h1><p>{subtitle}</p></div>{action}</div>
}
