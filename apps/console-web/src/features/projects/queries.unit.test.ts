import { describe, expect, it } from "vitest";

import { applicationKeys } from "../applications/queries";
import { environmentKeys } from "../environments/queries";
import { projectKeys } from "./queries";

describe("domain-owned query keys", () => {
  it("isolates tenant and parent resource scopes", () => {
    const workspace = "ws-aaaaaaaaaaaaaaaaaaaa";
    expect(projectKeys.list(workspace)).not.toEqual(projectKeys.list("ws-bbbbbbbbbbbbbbbbbbbb"));
    expect(applicationKeys.list(workspace, "prj-aaaaaaaaaaaaaaaaaaaa")).not.toEqual(
      applicationKeys.list(workspace, "prj-bbbbbbbbbbbbbbbbbbbb"),
    );
    expect(environmentKeys.list(workspace, "prj-aaaaaaaaaaaaaaaaaaaa")).not.toEqual(
      environmentKeys.list(workspace, "prj-bbbbbbbbbbbbbbbbbbbb"),
    );
  });
});
