import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { push } from './lib/push.svelte'

void push.register()

export default mount(App, { target: document.getElementById('app')! })
