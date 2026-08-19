import '@ant-design/v5-patch-for-react-19'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { ConfigProvider, App as AntApp, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.darkAlgorithm,
        token: { colorPrimary: '#e8a33d', colorBgBase: '#14181d' },
      }}
    >
      <AntApp>
        <App />
      </AntApp>
    </ConfigProvider>
  </React.StrictMode>,
)
