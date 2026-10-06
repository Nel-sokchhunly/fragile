import React from 'react'
import {createRoot} from 'react-dom/client'
import './index.css'
import App from './App'
import {subscribeEvents} from '@/lib/events'
import {useAppStore} from '@/store/app'

subscribeEvents()
void useAppStore.getState().init()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App/>
  </React.StrictMode>
)
