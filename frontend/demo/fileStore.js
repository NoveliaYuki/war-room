const DB_NAME = "war-room-demo-files-v1";

function openDatabase() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1);
    request.onupgradeneeded = () => request.result.createObjectStore("attachments");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(new Error("Browser file storage is unavailable."));
  });
}

function transact(mode, operation) {
  return openDatabase().then((database) => new Promise((resolve, reject) => {
    const transaction = database.transaction("attachments", mode);
    const store = transaction.objectStore("attachments");
    const result = operation(store);
    transaction.oncomplete = () => { database.close(); resolve(result?.result); };
    transaction.onerror = () => { database.close(); reject(new Error("Browser file storage failed.")); };
    transaction.onabort = () => { database.close(); reject(new Error("Browser file storage failed.")); };
  }));
}

/** Saves one attachment blob under its record ID. */
export function saveFile(id, blob) { return transact("readwrite", (store) => store.put(blob, id)); }

/** Reads one stored attachment blob. */
export function readFile(id) { return transact("readonly", (store) => store.get(id)); }

/** Deletes one attachment blob. */
export function deleteFile(id) { return transact("readwrite", (store) => store.delete(id)); }

/** Replaces all stored attachments after a validated import. */
export function replaceFiles(files) {
  return transact("readwrite", (store) => {
    store.clear();
    for (const [id, blob] of files) store.put(blob, id);
  });
}
