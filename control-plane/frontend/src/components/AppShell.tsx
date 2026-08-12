import type { ReactNode } from 'react'
import { Icon } from './Icon'

export type Section = 'Overview' | 'Nodes' | 'Users' | 'Boards' | 'Traffic' | 'Access'
const sections: Section[] = ['Overview', 'Nodes', 'Users', 'Boards', 'Traffic', 'Access']

export function AppShell({ section, onSection, topbar, children }: {
  section: Section
  onSection: (section: Section) => void
  topbar: ReactNode
  children: ReactNode
}) {
  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><span className="brand-mark">◇</span><span><strong>BoardProxy</strong><small>Control Plane</small></span></div>
      <nav aria-label="Primary navigation">
        {sections.map(item => <button aria-label={item} className={section === item ? 'nav-row active' : 'nav-row'} key={item} onClick={() => onSection(item)}>
          <Icon name={item.toLowerCase()} /><span>{item}</span>
        </button>)}
      </nav>
      <div className="sidebar-collapse">«</div>
    </aside>
    <div className="workspace">{topbar}<main>{children}</main></div>
  </div>
}
