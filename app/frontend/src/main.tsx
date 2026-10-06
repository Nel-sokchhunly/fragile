import React from 'react'
import {createRoot} from 'react-dom/client'
import './index.css'
import App from './App'
import {subscribeEvents} from '@/lib/events'

subscribeEvents()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App/>
  </React.StrictMode>
)
