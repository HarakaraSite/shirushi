const http = require('node:http');

const host = process.env.SHIRUSHI_E2E_404_HOST || '127.0.0.1';
const port = Number(process.env.SHIRUSHI_E2E_404_PORT || 18182);

const server = http.createServer((request, response) => {
  if (request.url.startsWith('/missing')) {
    setTimeout(() => {
      response.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' });
      response.end('not found');
    }, 1500);
    return;
  }

  response.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8' });
  response.end('ok');
});

server.listen(port, host, () => {
  console.log(`404 fixture started: http://${host}:${port}`);
});
