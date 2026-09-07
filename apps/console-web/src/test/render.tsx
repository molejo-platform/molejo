import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type RenderResult, render } from "@testing-library/react";
import type { ReactElement } from "react";

export function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false, gcTime: 0 },
    },
  });
}

export function renderApp(
  element: ReactElement,
  queryClient = createTestQueryClient(),
): RenderResult & { queryClient: QueryClient; rerenderApp: (next: ReactElement) => void } {
  const wrap = (child: ReactElement) => <QueryClientProvider client={queryClient}>{child}</QueryClientProvider>;
  const result = render(wrap(element));
  return { ...result, queryClient, rerenderApp: (next) => result.rerender(wrap(next)) };
}

export function renderWithQueryClient(
  element: ReactElement,
): ReturnType<typeof renderApp> & { rerenderWithQueryClient: (next: ReactElement) => void } {
  const result = renderApp(element);
  return { ...result, rerenderWithQueryClient: result.rerenderApp };
}
