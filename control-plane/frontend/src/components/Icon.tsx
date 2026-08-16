import type { Section } from '../types'

type IconName = Section | 'search' | 'refresh' | 'chevron' | 'close' | 'plus' | 'copy' | 'warning' | 'check' | 'logout' | 'more'

const paths: Record<IconName, React.ReactNode> = {
  overview: <><rect x="3.5" y="3.5" width="6" height="6"/><rect x="14.5" y="3.5" width="6" height="6"/><rect x="3.5" y="14.5" width="6" height="6"/><rect x="14.5" y="14.5" width="6" height="6"/></>,
  nodes: <><rect x="4" y="5" width="16" height="5" rx="1.5"/><rect x="4" y="14" width="16" height="5" rx="1.5"/><path d="M7 7.5h.01M7 16.5h.01"/></>,
  subscriptions: <><path d="M5 12a7 7 0 0 1 7-7M5 17a12 12 0 0 1 12-12"/><circle cx="6" cy="18" r="1.4" fill="currentColor" stroke="none"/><rect x="13" y="13" width="7" height="7" rx="1.6"/></>,
  users: <><circle cx="9" cy="8" r="3.2"/><path d="M3.5 19c.6-3.2 2.8-4.8 5.5-4.8S14 15.8 14.6 19M16 6.2a3 3 0 0 1 0 5.6M18 19c-.3-2-.9-3.4-2-4.3"/></>,
  boards: <><rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M3.5 9.5h17M9 9.5v10"/></>,
  traffic: <><path d="M4 19V5M4 19h16M7.5 15.5l3.5-4.5 3 2.5 4.5-6"/></>,
  activity: <path d="M3 12h4l2.5-6 4 12L16 12h5"/>,
  access: <><path d="M12 3.5l7 3v5c0 4-2.9 7.4-7 9-4.1-1.6-7-5-7-9v-5z"/><path d="M9.5 12.2l1.9 1.9 3.4-3.6"/></>,
  search: <><circle cx="10.5" cy="10.5" r="6"/><path d="M15 15l4.5 4.5"/></>,
  refresh: <path d="M20 12a8 8 0 1 1-2.4-5.7M20 4.5V10h-5.4"/>,
  chevron: <path d="M6 9.5l6 6 6-6"/>, close: <path d="m6 6 12 12M18 6 6 18"/>, plus: <path d="M12 5v14M5 12h14"/>,
  copy: <><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></>,
  warning: <><path d="M12 4 3.5 19h17z"/><path d="M12 9v4M12 16.5h.01"/></>, check: <path d="m5 12 4 4L19 6"/>,
  logout: <><path d="M10 5H5v14h5M14 8l4 4-4 4M8 12h10"/></>, more: <><circle cx="5" cy="12" r="1" fill="currentColor"/><circle cx="12" cy="12" r="1" fill="currentColor"/><circle cx="19" cy="12" r="1" fill="currentColor"/></>,
}

export function Icon({ name, size = 18 }: { name: IconName; size?: number }) {
  return <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>
}
