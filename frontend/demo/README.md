# Static browser demo

The demo and backend app use the same `../index.html`, boot script, styles, and UI modules. Only `runtime-config.json` and the selected data adapter differ. The `?demo` query parameter can also select demo mode on a local backend build.

For Cloudflare Pages, set the project root to `frontend`, build command to `npm run build:demo`, and build output directory to `dist-demo`. The build copies `index.html` and all shared UI files unchanged, then writes a runtime config selecting the browser-only demo adapter. The output is static and does not include the Go backend.

Company logos used by the sample records are bundled in `assets/logos`, so they load as static assets with no external image requests. Demo processes and imported logos are saved in local storage, while uploaded files are saved in IndexedDB. Clear `war-room-demo-data-v13` and `war-room-demo-company-logos-v1` in local storage and `war-room-demo-files-v1` in IndexedDB to restore the initial sample data.

The static demo uses the same ZIP backup format and Data dialog as the local app. Backups include processes, company icons, and uploaded files. Sample edits remain in the current browser until exported or cleared.
