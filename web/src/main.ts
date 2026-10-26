import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { startRouter } from './router/router'

startRouter()

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
