import daisyui from 'daisyui';
import type { Config } from 'tailwindcss';

const config: Config = {
  content: ['./src/**/*.{ts,tsx}'],
  // Preflight stays off so the existing CSS Modules and globals.css render unchanged.
  corePlugins: { preflight: false },
  plugins: [daisyui],
  daisyui: {
    themes: ['winter', 'night'],
    // The root background and colour come from globals.css, not from daisyUI.
    base: false,
    logs: false,
  },
};

export default config;
