import { createRoot } from "react-dom/client";
import "@fontsource-variable/ibm-plex-sans";
import "@fontsource-variable/space-grotesk";
import "@fontsource/ibm-plex-mono/latin-400.css";
import "@fontsource/ibm-plex-mono/latin-600.css";
import "@fontsource/ibm-plex-mono/latin-700.css";

import { AppProviders } from "./app/providers";
import { createAppRouter } from "./app/router";
import { createQueryClient } from "./shared/query/query-client";
import { RouterProvider } from "@tanstack/react-router";
import "./shared/styles.css";

const queryClient = createQueryClient();
const router = createAppRouter(queryClient);

createRoot(document.getElementById("root")!).render(<AppProviders queryClient={queryClient}><RouterProvider router={router} /></AppProviders>);
