import { useWindowStore } from './windowStore'
import { tt } from '../lib/i18n'

interface TaskbarProps {
  onStartClick: () => void
}

export function Taskbar({ onStartClick }: TaskbarProps) {
  const windows = useWindowStore((s) => s.windows)
  const focus = useWindowStore((s) => s.focus)

  return (
    <div className="taskbar">
      <div className="start-btn" title={tt('应用中心')} onClick={onStartClick}>
        🪟
      </div>
      {windows.map((w) => (
        <div
          key={w.id}
          className={`task-item${w.minimized ? '' : ' active'}`}
          title={w.title}
          onClick={() => focus(w.id)}
        >
          <span>{w.icon}</span>
          {!w.minimized && <span className="indicator" />}
        </div>
      ))}
    </div>
  )
}
