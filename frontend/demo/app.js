import { demoApi } from "./demoStore.js";
import { useApiAdapter } from "../public/js/api.js";

useApiAdapter(demoApi);
document.body.dataset.demo = "true";
await import("../public/js/app.js");
