import { createRoot } from "react-dom/client";

import { AppProviders } from "./app/providers";
import { createAppRouter } from "./app/router";
import { createQueryClient } from "./shared/query/query-client";
import { RouterProvider } from "@tanstack/react-router";
import "./shared/styles.css";

const queryClient = createQueryClient();
const router = createAppRouter(queryClient);

createRoot(document.getElementById("root")!).render(<AppProviders queryClient={queryClient}><RouterProvider router={router} /></AppProviders>);
