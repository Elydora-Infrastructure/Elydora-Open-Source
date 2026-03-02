/**
 * Redis cache adapter implementing the Cache interface.
 *
 * Uses ioredis to back the nonce-deduplication and chain-hash caching
 * that was previously served by Cloudflare KVNamespace.
 */

import { Redis } from 'ioredis';
import type { Cache } from './interfaces.js';

export class RedisCacheAdapter implements Cache {
  private readonly client: Redis;

  constructor(redisUrl: string) {
    this.client = new Redis(redisUrl, { lazyConnect: true });
  }

  async get(key: string): Promise<string | null> {
    return this.client.get(key);
  }

  async put(
    key: string,
    value: string,
    options?: { expirationTtl?: number },
  ): Promise<void> {
    if (options?.expirationTtl) {
      await this.client.set(key, value, 'EX', options.expirationTtl);
    } else {
      await this.client.set(key, value);
    }
  }

  async connect(): Promise<void> {
    await this.client.connect();
  }

  async close(): Promise<void> {
    await this.client.quit();
  }
}
