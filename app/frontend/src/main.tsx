import React from 'react'
import {createRoot} from 'react-dom/client'
import './index.css'
import App from './App'
import {subscribeEvents} from '@/lib/events'
import {subscribeTerminal} from '@/lib/terminal'
import {useAppStore} from '@/store/app'

subscribeEvents()
subscribeTerminal()
void useAppStore.getState().init()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App/>
  </React.StrictMode>
)
