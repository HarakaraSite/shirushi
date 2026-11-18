const http = require('node:http');

const host = process.env.SHIRUSHI_E2E_404_HOST || '127.0.0.1';
const port = Number(process.env.SHIRUSHI_E2E_404_PORT || 18182);
const thumbnailPNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPY7yANAALcARu6z885AAAAAElFTkSuQmCC', 'base64');

const server = http.createServer((request, response) => {
  if (request.url.split('?')[0] === '/thumbnail.png') {
    response.writeHead(200, { 'Content-Type': 'image/png' });
    response.end(thumbnailPNG);
    return;
  }

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
