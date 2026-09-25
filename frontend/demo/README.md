# Static browser demo

The demo shares `../index.html`, the styles, and UI components with the backend app. It loads the fake dataset and browser-only store when the URL contains `?demo`.

For Cloudflare Pages, set the project root to `frontend`, build command to `npm run build:demo`, and build output directory to `dist-demo`. The build rewrites the shared page to load demo mode by default; the output is static and does not include the Go backend.

Company logos used by the sample records are bundled in `assets/logos`, so they load as static assets with no external image requests. Demo changes are stored in the visitor's local browser storage. Clear `war-room-demo-data-v13` in browser storage to restore the initial sample data. File attachments are unavailable in the static demo.
