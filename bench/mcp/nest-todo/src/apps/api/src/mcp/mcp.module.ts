import { Module } from '@nestjs/common';
import { HealthController, McpController } from './mcp.controller';
import { McpAuthGuard } from './mcp-auth.guard';
import { McpServerService } from './mcp-server.service';
import { McpSessionService } from './mcp-session.service';

@Module({
  controllers: [McpController, HealthController],
  providers: [McpAuthGuard, McpServerService, McpSessionService],
})
export class McpModule {}
