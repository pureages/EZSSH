import { useEffect, useState } from 'react'
import { HashRouter, Navigate, Route, Routes } from 'react-router-dom'
import { LoginGate } from './pages/LoginGate'
import { HostsPage } from './pages/HostsPage'
import { DesktopPage } from './pages/DesktopPage'
import { api } from './lib/api'
import { useSession } from './lib/session'
import { registerApp } from './desktop/appRegistry'
import { TerminalApp } from './apps/TerminalApp'
import { FileManagerApp } from './apps/FileManagerApp'
import { TaskManagerApp } from './apps/TaskManagerApp'
import { DockerApp } from './apps/DockerApp'
import { FirewallApp } from './apps/FirewallApp'
import { SettingsApp } from './apps/SettingsApp'
import { DownloadApp } from './apps/DownloadApp'
import { OneClickCmdApp } from './apps/OneClickCmdApp'
import { WebsiteApp } from './apps/WebsiteApp'
import { tt, useI18n } from './lib/i18n'
import { DEFAULT_LOGIN_ROUTE } from './lib/routes'
import { GlobalContextMenu } from './components/GlobalContextMenu'

// 注册内置应用
registerApp({
  id: 'terminal',
  name: '终端',
  icon: '🖥️',
  defaultSize: { width: 820, height: 520 },
  needsHost: true,
  component: TerminalApp,
})
registerApp({
  id: 'files',
  name: '文件管理器',
  icon: '📁',
  defaultSize: { width: 1160, height: 700 },
  needsHost: true,
  component: FileManagerApp,
})
registerApp({
  id: 'taskmanager',
  name: '任务管理器',
  icon: '📊',
  defaultSize: { width: 1120, height: 680 },
  needsHost: true,
  singleton: true,
  component: TaskManagerApp,
})
registerApp({
  id: 'docker',
  name: 'Docker 管理',
  icon: '🐳',
  defaultSize: { width: 1000, height: 620 },
  needsHost: true,
  singleton: true,
  platforms: ['linux'],
  disabledTip: 'windows的docker不可用。',
  component: DockerApp,
})
registerApp({
  id: 'firewall',
  name: '防火墙',
  icon: '🧱',
  defaultSize: { width: 1020, height: 660 },
  needsHost: true,
  singleton: true,
  component: FirewallApp,
})
registerApp({
  id: 'settings',
  name: '设置',
  icon: '⚙️',
  defaultSize: { width: 760, height: 560 },
  singleton: true,
  component: SettingsApp,
})
registerApp({
  id: 'download',
  name: '直链下载',
  icon: '⬇️',
  defaultSize: { width: 1000, height: 640 },
  needsHost: true,
  component: DownloadApp,
})
registerApp({
  id: 'oneclick',
  name: '一键命令',
  icon: '⚡',
  defaultSize: { width: 920, height: 660 },
  singleton: true,
  component: OneClickCmdApp,
})
registerApp({
  id: 'website',
  name: '网站管理',
  icon: '🌐',
  defaultSize: { width: 1160, height: 700 },
  singleton: true,
  component: WebsiteApp,
})

function App() {
  const authed = useSession((s) => s.authed)
  const setAuthed = useSession((s) => s.setAuthed)
  const setLang = useI18n((s) => s.setLang)
  const [routeReady, setRouteReady] = useState(false)

  useEffect(() => {
    // 启动时校验会话 + 应用服务器端语言偏好
    // （注意：安全路由的值不再下发，登录入口由 LoginGate 通过 /api/route-check 判定）
    api
      .initStatus()
      .then((s) => {
        setLang(s.lang === 'zh' ? 'zh' : 'en')
        setRouteReady(true)
      })
      .catch(() => setRouteReady(true))
    api
      .me()
      .then(() => setAuthed(true))
      .catch(() => setAuthed(false))
  }, [setAuthed, setLang])

  if (authed === null || !routeReady) {
    return (
      <>
        <GlobalContextMenu />
        <div className="auth-page">
          <div className="auth-card" style={{ textAlign: 'center', color: 'var(--text-1)' }}>
            {tt('加载中…')}
          </div>
        </div>
      </>
    )
  }

  return (
    <>
      <GlobalContextMenu />
      <HashRouter>
      <Routes>
        <Route
          path="/desktop"
          element={authed ? <DesktopPage /> : <Navigate to={DEFAULT_LOGIN_ROUTE} replace />}
        />
        <Route
          path="/hosts"
          element={authed ? <HostsPage /> : <Navigate to={DEFAULT_LOGIN_ROUTE} replace />}
        />
        {/*
          其它所有路径（含首页 / 与手动输入的安全路由）都交给登录闸门：
          由 POST /api/route-check 判定当前路径能否展示登录页，服务端不下发安全路由的值。
        */}
        <Route
          path="*"
          element={authed ? <Navigate to="/desktop" replace /> : <LoginGate />}
        />
      </Routes>
      </HashRouter>
    </>
  )
}

export default App
