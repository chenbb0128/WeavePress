import { defineConfig, viteCssLayerPlugin } from '@vben/vite-config';

import ElementPlus from 'unplugin-element-plus/vite';

export default defineConfig(async () => {
  return {
    application: {
      license: false,
      print: false,
      pwa: false,
      vxeTableLazyImport: false,
    },
    vite: {
      plugins: [
        viteCssLayerPlugin({ layerName: 'el', packageName: 'element-plus' }),
        ElementPlus({ format: 'esm' }),
      ],
      server: {
        proxy: {
          '/api': {
            changeOrigin: true,
            target: 'http://localhost:8080',
            ws: true,
          },
          '/media': {
            changeOrigin: true,
            target: 'http://localhost:8080',
          },
        },
      },
    },
  };
});
