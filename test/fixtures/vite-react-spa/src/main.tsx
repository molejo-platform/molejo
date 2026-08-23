import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "./styles.css";

const path = window.location.pathname;
const version = import.meta.env.VITE_APP_VERSION || "dev";
const title = path === "/projects/example" ? "Example project" : "Frontend fixture";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <main>
      <p className="eyebrow">vite-react-spa</p>
      <h1>{title}</h1>
      <dl>
        <div>
          <dt>Version</dt>
          <dd>{version}</dd>
        </div>
        <div>
          <dt>Route</dt>
          <dd>{path}</dd>
        </div>
      </dl>
    </main>
  </StrictMode>,
);
