import { createApp, defineCustomElement, h } from 'vue'
import App from './App.vue'

const TAG = 'kedge-provider-sandbox'

const Element = defineCustomElement({
  render: () => h(App),
})

if (!customElements.get(TAG)) {
  customElements.define(TAG, Element)
}

const mount = document.querySelector(TAG)
if (mount) createApp(App).mount(mount)
