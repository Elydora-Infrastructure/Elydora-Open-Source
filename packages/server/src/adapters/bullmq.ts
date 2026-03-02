/**
 * BullMQ queue adapter implementing the MessageQueue interface.
 *
 * Wraps a BullMQ Queue instance to replace Cloudflare's Queue producer.
 * Jobs are enqueued with retry-on-failure semantics (3 attempts,
 * exponential back-off) matching the Cloudflare Queues default behaviour.
 */

import { Queue as BullQueue } from 'bullmq';
import type { ConnectionOptions } from 'bullmq';
import type { MessageQueue } from './interfaces.js';
import { generateUUIDv7 } from '../utils/uuid.js';

export const QUEUE_NAME = 'elydora-queue';

export class BullMQAdapter implements MessageQueue {
  private readonly queue: BullQueue;

  constructor(connection: ConnectionOptions) {
    this.queue = new BullQueue(QUEUE_NAME, { connection });
  }

  async send(body: unknown): Promise<{ messageId: string }> {
    const job = await this.queue.add('message', body as Record<string, unknown>, {
      attempts: 3,
      backoff: { type: 'exponential', delay: 1000 },
      removeOnComplete: { count: 100 },
      removeOnFail: { count: 500 },
    });
    return { messageId: job.id ?? generateUUIDv7() };
  }

  async close(): Promise<void> {
    await this.queue.close();
  }
}
