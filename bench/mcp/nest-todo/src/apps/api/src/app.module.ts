import { Module } from '@nestjs/common';
import { APP_GUARD, APP_INTERCEPTOR } from '@nestjs/core';
import { ThrottlerGuard, ThrottlerModule } from '@nestjs/throttler';
import { McpModule } from './mcp/mcp.module';
import { TimeoutInterceptor } from './timeout.interceptor';

const RATE_LIMIT = 300;
const RATE_WINDOW_MS = 60_000;

@Module({
  imports: [
    // 300 requests per 60 seconds per client IP. RATE_LIMIT_ENABLED=false
    // turns it off (the benchmark drives the server from one IP).
    ThrottlerModule.forRoot({
      throttlers: [{ ttl: RATE_WINDOW_MS, limit: RATE_LIMIT }],
      skipIf: () => process.env.RATE_LIMIT_ENABLED === 'false',
    }),
    McpModule,
  ],
  providers: [
    { provide: APP_GUARD, useClass: ThrottlerGuard },
    { provide: APP_INTERCEPTOR, useClass: TimeoutInterceptor },
  ],
})
export class AppModule {}
