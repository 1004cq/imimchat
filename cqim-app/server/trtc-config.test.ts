import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { CANONICAL_TRTC_SDK_APP_ID, resolveTrtcSdkAppId } from "./trtc-config.ts";

describe("resolveTrtcSdkAppId", () => {
  it("requires a positive integer env value", () => {
    assert.equal(resolveTrtcSdkAppId(undefined), null);
    assert.equal(resolveTrtcSdkAppId(null), null);
    assert.equal(resolveTrtcSdkAppId(""), null);
    assert.equal(resolveTrtcSdkAppId("   "), null);
    assert.equal(resolveTrtcSdkAppId("abc"), null);
    assert.equal(resolveTrtcSdkAppId("0"), null);
    assert.equal(resolveTrtcSdkAppId("-1"), null);
    assert.equal(resolveTrtcSdkAppId("1.5"), null);
  });

  it("uses only the configured id", () => {
    assert.equal(resolveTrtcSdkAppId("1600159677"), CANONICAL_TRTC_SDK_APP_ID);
    assert.equal(resolveTrtcSdkAppId(" 1600159677 "), CANONICAL_TRTC_SDK_APP_ID);
  });
});
