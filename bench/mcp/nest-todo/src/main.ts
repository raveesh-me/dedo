import 'reflect-metadata';
import { Logger } from '@nestjs/common';
import { NestFactory } from '@nestjs/core';
import type { NestExpressApplication } from '@nestjs/platform-express';
import { AppModule } from './apps/api/src/app.module';

const DEFAULT_PORT = 3100;
const MAX_BODY = '10mb';

async function bootstrap(): Promise<void> {
  const app = await NestFactory.create<NestExpressApplication>(AppModule, { bodyParser: false });
  app.useBodyParser('json', { limit: MAX_BODY });
  // CORS: only the origins in CORS_ORIGINS (comma-separated); none by default.
  app.enableCors({
    origin: (process.env.CORS_ORIGINS ?? '').split(',').map((o) => o.trim()).filter(Boolean),
    methods: ['GET', 'POST', 'DELETE'],
    allowedHeaders: ['Authorization', 'Content-Type', 'Accept', 'Mcp-Session-Id', 'Mcp-Protocol-Version', 'Last-Event-ID', 'X-Flow-ID'],
    exposedHeaders: ['Mcp-Session-Id'],
  });
  app.enableShutdownHooks();
  const port = Number(process.env.API_PORT ?? DEFAULT_PORT);
  await app.listen(port, '0.0.0.0');
  Logger.log(`MCP server listening on :${port}/mcp`, 'Bootstrap');
}

void bootstrap();
