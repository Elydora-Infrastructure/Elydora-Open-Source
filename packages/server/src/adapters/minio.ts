/**
 * MinIO / S3-compatible object storage adapter implementing the ObjectStore interface.
 *
 * Uses the AWS SDK v3 S3 client configured with a custom endpoint for MinIO.
 * Responses are buffered in memory so that both streaming (body) and random
 * access (json/text) are available on the returned ObjectStoreObject.
 */

import {
  S3Client,
  PutObjectCommand,
  GetObjectCommand,
  HeadObjectCommand,
  type PutObjectCommandInput,
} from '@aws-sdk/client-s3';
import { Readable } from 'stream';
import type {
  ObjectStore,
  ObjectStoreObject,
  ObjectStoreHead,
  ObjectStorePutOptions,
} from './interfaces.js';

// ---------------------------------------------------------------------------
// S3 error detection
// ---------------------------------------------------------------------------

function isNoSuchKey(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false;
  const code = (err as Record<string, unknown>).Code ?? (err as Record<string, unknown>).name;
  return code === 'NoSuchKey' || code === 'NotFound';
}

// ---------------------------------------------------------------------------
// MinIO adapter
// ---------------------------------------------------------------------------

export class MinioAdapter implements ObjectStore {
  private readonly client: S3Client;
  private readonly bucket: string;

  constructor(endpoint: string, accessKey: string, secretKey: string, bucket: string) {
    this.client = new S3Client({
      endpoint,
      region: 'us-east-1', // required by S3 SDK, MinIO ignores it
      credentials: {
        accessKeyId: accessKey,
        secretAccessKey: secretKey,
      },
      forcePathStyle: true, // required for MinIO
    });
    this.bucket = bucket;
  }

  async put(
    key: string,
    body: string | Uint8Array | ReadableStream,
    options?: ObjectStorePutOptions,
  ): Promise<void> {
    let bodyData: Buffer | string | Uint8Array;

    if (typeof body === 'string') {
      bodyData = body;
    } else if (body instanceof Uint8Array) {
      bodyData = body;
    } else {
      // Web ReadableStream — collect into buffer
      const chunks: Uint8Array[] = [];
      const reader = body.getReader();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        if (value) chunks.push(value);
      }
      bodyData = Buffer.concat(chunks);
    }

    const params: PutObjectCommandInput = {
      Bucket: this.bucket,
      Key: key,
      Body: bodyData,
    };

    if (options?.httpMetadata?.contentType) {
      params.ContentType = options.httpMetadata.contentType;
    }

    if (options?.customMetadata) {
      params.Metadata = options.customMetadata;
    }

    await this.client.send(new PutObjectCommand(params));
  }

  async get(key: string): Promise<ObjectStoreObject | null> {
    try {
      const response = await this.client.send(
        new GetObjectCommand({ Bucket: this.bucket, Key: key }),
      );

      if (!response.Body) return null;

      // Buffer the entire response so that json(), text(), and body can all work
      const nodeReadable = response.Body as Readable;
      const chunks: Buffer[] = [];
      for await (const chunk of nodeReadable) {
        chunks.push(chunk as Buffer);
      }
      const buffer = Buffer.concat(chunks);
      const contentType = response.ContentType;
      const contentLength = response.ContentLength;

      return {
        get body(): ReadableStream {
          return new ReadableStream({
            start(controller) {
              controller.enqueue(buffer);
              controller.close();
            },
          });
        },
        size: contentLength,
        httpMetadata: contentType ? { contentType } : undefined,
        async json<T = unknown>(): Promise<T> {
          return JSON.parse(buffer.toString('utf-8')) as T;
        },
        async text(): Promise<string> {
          return buffer.toString('utf-8');
        },
      };
    } catch (err) {
      if (isNoSuchKey(err)) return null;
      throw err;
    }
  }

  async head(key: string): Promise<ObjectStoreHead | null> {
    try {
      const response = await this.client.send(
        new HeadObjectCommand({ Bucket: this.bucket, Key: key }),
      );
      return {
        size: response.ContentLength,
        httpMetadata: response.ContentType ? { contentType: response.ContentType } : undefined,
      };
    } catch (err) {
      if (isNoSuchKey(err)) return null;
      throw err;
    }
  }
}
