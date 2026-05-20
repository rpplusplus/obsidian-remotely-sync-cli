# Design: remotely-save Compatibility

This document records technical details about how remotely-save stores and retrieves encrypted files, and how this CLI maintains compatibility.

## Encrypted Filename Behavior

### remotely-save's approach

remotely-save encrypts filenames using AES-256-CBC with a random 8-byte salt. Each encryption produces a different ciphertext even for the same input. However, remotely-save **caches** the encrypted filename in memory (`cacheMapOrigToEnc`), so when it updates a file, it reuses the same encrypted key and overwrites the S3 object in place.

From `fsEncrypt.ts`:
```javascript
let keyEnc = this.cacheMapOrigToEnc[key];  // check cache first
if (keyEnc === undefined) {
    keyEnc = await this._encryptName(key);  // encrypt only if new
    this.cacheMapOrigToEnc[key] = keyEnc;   // cache for reuse
}
// subsequent writes reuse the same key
```

### This CLI's approach

We store the encrypted filename in the state DB (`remote_hash` column). On push:

1. Check state DB for existing encrypted name for this file
2. If found → reuse it (overwrite the S3 object in place)
3. If not found → encrypt a new name (new file)

This ensures only one S3 object exists per logical file, matching remotely-save's behavior.

### Why this matters

Without this, each push would create a new S3 object with a different encrypted key. The old object would remain, and remotely-save (listing all objects and decrypting names) would see multiple objects with the same decrypted filename. It would pick one based on iteration order — typically the first encountered, which could be the oldest version.

## File Discovery

remotely-save discovers files by:
1. Listing all objects in the S3 bucket (`ListObjectsV2`)
2. Decrypting each object key to recover the original filename
3. Building a filename → encrypted-key mapping

There is no separate metadata or index file for the mapping. The encrypted filename IS the S3 key.

## Encryption Parameters

| Parameter | Value |
|-----------|-------|
| Algorithm | AES-256-CBC |
| KDF | PBKDF2-SHA256 |
| Iterations | 20,000 |
| Salt | 8 bytes (random) |
| Key length | 32 bytes |
| IV length | 16 bytes |
| Padding | PKCS#7 |
| Header | `Salted__` + salt + ciphertext |
| Filename encoding | base64url (no padding) |

## S3 Compatibility

remotely-save uses S3 API directly (not CDN). S3 is strongly consistent — after a PUT, subsequent GETs and LISTs return the latest version. CDN caching is not a factor for sync operations.
