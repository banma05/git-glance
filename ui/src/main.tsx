import { render } from 'preact';
import { App } from './components/App';
import { pickHost } from './host/types';
import './theme.css';
import './app.css';

// The host is created first: the browser host takes the token out of
// location.hash when it is created, before anything else reads the address.
const host = pickHost();
render(<App host={host} />, document.getElementById('app')!);
