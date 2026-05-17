import './app.css';
import App from './App.svelte';
import { mount } from 'svelte';
import { setupTheme } from './theme';

setupTheme();

const app = mount(App, {
  target: document.getElementById('app') as HTMLElement
});

export default app;
