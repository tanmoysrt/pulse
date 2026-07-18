import { createRouter, createWebHashHistory } from 'vue-router'
import Home from './views/Home.vue'
import Session from './views/Session.vue'
import History from './views/History.vue'

const routes = [
  { path: '/', name: 'home', component: Home },
  { path: '/s/:id', name: 'session', component: Session, props: true },
  { path: '/h/:ref', name: 'history', component: History, props: true },
]

export default createRouter({ history: createWebHashHistory(), routes })
