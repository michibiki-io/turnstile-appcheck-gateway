/// <reference types="vite/client" />

declare global {
  interface Window {
    TACG_ADMIN?: {
      apiBasePath: string;
      dashboardBasePath: string;
    };
  }
}

export {};
