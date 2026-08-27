import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import type { ReactElement } from "react";

export function renderWithQueryClient(element: ReactElement): RenderResult & { rerenderWithQueryClient: (next: ReactElement) => void } {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrap = (child: ReactElement) => <QueryClientProvider client={queryClient}>{child}</QueryClientProvider>;
  const result = render(wrap(element));
  return { ...result, rerenderWithQueryClient: (next) => result.rerender(wrap(next)) };
}
