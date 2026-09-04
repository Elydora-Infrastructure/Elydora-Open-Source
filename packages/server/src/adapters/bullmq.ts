/** BullMQ queue adapter implementing the MessageQueue interface. */

import { Queue as BullQueue } from 'bullmq';
import type { ConnectionOptions } from 'bullmq';
import type { MessageQueue } from './interfaces.js';

export const QUEUE_NAME = 'elydora-queue';

export class BullMQAdapter implements MessageQueue {
  private readonly queue: BullQueue;

  constructor(connection: ConnectionOptions) {
    this.queue = new BullQueue(QUEUE_NAME, { connection });
  }

  async send(messageId: string, body: unknown): Promise<void> {
    await this.queue.add('message', body as Record<string, unknown>, {
      jobId: messageId,
      attempts: 3,
      backoff: { type: 'exponential', delay: 1000 },
      removeOnComplete: { count: 100 },
      removeOnFail: { count: 500 },
    });
  }

  async close(): Promise<void> {
    await this.queue.close();
  }
}
