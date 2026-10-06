// The dev server (npm start) serves the console at / and passes the API to a
// local bridge's console listener (BRIDGE_CONSOLE_ADDR, :8090 by default in
// console mode).
const target = process.env.BRIDGE_CONSOLE_URL ?? 'http://127.0.0.1:8090';

export default {
  '/api': { target, secure: false, changeOrigin: false },
};
