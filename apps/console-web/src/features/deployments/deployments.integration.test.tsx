import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { DeploymentState } from "../../shared/api/types";
import { DeploymentStatus } from "./DeploymentStatus";

afterEach(cleanup);

describe("deployment components", () => {
  it.each([
    ["Pending", "Pending"],
    ["Progressing", "Progressing"],
    ["Ready", "Pronto"],
    ["Degraded", "Degraded"],
    ["Unknown", "Desconhecido"],
  ] as const)("renders %s as a distinct product state", (state, label) => {
    render(<DeploymentStatus state={state as DeploymentState} />);
    expect(screen.getByText(label)).toBeTruthy();
  });
});
