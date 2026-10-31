import { delMany, getMany, keys } from 'idb-keyval'

export interface RecentCast {
  id: string
  name: string
  size: number
  opened: number
}

type LegacyCast = RecentCast
let database: Promise<IDBDatabase> | undefined

function request<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

function completed(tx: IDBTransaction): Promise<void> {
  const promise = new Promise<void>((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onabort = tx.onerror = () => reject(tx.error ?? new Error('Storage transaction aborted'))
  })
  // A request can fail before its caller reaches the transaction's completion.
  promise.catch(() => {})
  return promise
}

function connect(): Promise<IDBDatabase> {
  if (!database) {
    database = new Promise<IDBDatabase>((resolve, reject) => {
      const req = indexedDB.open('reel-inspector', 1)
      req.onupgradeneeded = () => {
        const db = req.result
        db.createObjectStore('casts', { keyPath: 'name' }).createIndex('opened', 'opened')
        db.createObjectStore('bytes')
        db.createObjectStore('settings')
      }
      req.onsuccess = () => {
        req.result.onversionchange = () => req.result.close()
        resolve(req.result)
      }
      req.onerror = () => reject(req.error)
    }).then(async (db) => {
      try {
        await migrate(db)
        return db
      }
      catch (err) {
        db.close()
        throw err
      }
    }).catch((err) => {
      database = undefined
      throw err
    })
  }
  return database
}

// Both stores participate in every mutation, so readers never see metadata
// without bytes, including when two tabs replace a name at the same time.
async function prune(tx: IDBTransaction) {
  const store = tx.objectStore('casts')
  const records = await request<RecentCast[]>(store.index('opened').getAll())
  records.sort((a, b) => b.opened - a.opened || a.name.localeCompare(b.name))
  for (const r of records.slice(20)) {
    store.delete(r.name)
    tx.objectStore('bytes').delete(r.name)
  }
}

async function migrate(db: IDBDatabase) {
  const check = db.transaction('settings')
  if (await request(check.objectStore('settings').get('migrated'))) {
    await cleanLegacy(db)
    return
  }
  const legacyKeys = (await keys()).filter(k => k === 'recent' || (typeof k === 'string' && k.startsWith('cast:')))
  const values = await getMany(legacyKeys)
  const old = new Map(legacyKeys.map((k, i) => [k, values[i]]))
  const list = (old.get('recent') as LegacyCast[] | undefined) ?? []
  const newest = new Map<string, LegacyCast>()
  for (const r of list) {
    if (!newest.has(r.name) || newest.get(r.name)!.opened < r.opened)
      newest.set(r.name, r)
  }
  const tx = db.transaction(['casts', 'bytes', 'settings'], 'readwrite')
  const done = completed(tx)
  // The flag is read again under the write lock: simultaneous migrations
  // cannot re-import old data after another tab has finished.
  if (!await request(tx.objectStore('settings').get('migrated'))) {
    for (const r of newest.values()) {
      const bytes = old.get(`cast:${r.id}`) as Uint8Array | undefined
      const existing = await request<RecentCast | undefined>(tx.objectStore('casts').get(r.name))
      if (bytes && (!existing || existing.opened < r.opened)) {
        tx.objectStore('casts').put({ id: r.name, name: r.name, size: bytes.length, opened: r.opened })
        tx.objectStore('bytes').put(bytes, r.name)
      }
    }
    await prune(tx)
    tx.objectStore('settings').put(legacyKeys, 'legacyKeys')
    tx.objectStore('settings').put(true, 'migrated')
  }
  await done
  await cleanLegacy(db)
}

// Remember the keys until cleanup succeeds, including across a reload.
async function cleanLegacy(db: IDBDatabase) {
  const keys = await request<IDBValidKey[] | undefined>(db.transaction('settings').objectStore('settings').get('legacyKeys'))
  if (!keys)
    return
  await delMany(keys)
  const tx = db.transaction('settings', 'readwrite')
  const done = completed(tx)
  tx.objectStore('settings').delete('legacyKeys')
  await done
}

export async function listRecent(): Promise<RecentCast[]> {
  const db = await connect()
  const rows = await request<RecentCast[]>(db.transaction('casts').objectStore('casts').index('opened').getAll())
  return rows.sort((a, b) => b.opened - a.opened || a.name.localeCompare(b.name))
}

export async function storeCast(name: string, bytes: Uint8Array) {
  const db = await connect()
  const tx = db.transaction(['casts', 'bytes'], 'readwrite')
  const done = completed(tx)
  tx.objectStore('casts').put({ id: name, name, size: bytes.length, opened: Date.now() })
  tx.objectStore('bytes').put(bytes, name)
  await prune(tx)
  await done
}

export async function readCast(name: string): Promise<{ name: string, bytes: Uint8Array } | undefined> {
  const db = await connect()
  const tx = db.transaction(['casts', 'bytes'])
  const store = tx.objectStore('bytes')
  let bytes = await request<Uint8Array | undefined>(store.get(name))
  if (!bytes) {
    const old = /^(.+):\d+$/.exec(name)
    if (old) {
      name = old[1]!
      bytes = await request<Uint8Array | undefined>(store.get(name))
    }
  }
  return bytes ? { name, bytes } : undefined
}

export async function deleteCast(name: string) {
  const db = await connect()
  const tx = db.transaction(['casts', 'bytes'], 'readwrite')
  const done = completed(tx)
  tx.objectStore('casts').delete(name)
  tx.objectStore('bytes').delete(name)
  await done
}
