import { describe, expect, it } from "vitest";

import { channelBody, channelError } from "./NotificationChannels";

const form = { name: "운영", secret: "", chatId: " -100 ", recipients: "a@x.co, b@x.co", severities: ["critical" as const] };

describe("notification channel form", () => {
  it("builds provider-specific bodies and omits an empty secret", () => {
    expect(channelBody("telegram", { ...form, secret: "t" })).toEqual({
      provider: "telegram",
      display_name: "운영",
      config: { chat_id: "-100" },
      secret: "t",
      severities: ["critical"],
    });
    expect(channelBody("email_smtp", form).config).toEqual({ recipients: ["a@x.co", "b@x.co"] });
    expect(channelBody("discord", form)).not.toHaveProperty("secret");
  });

  it("explains error codes without echoing provider responses", () => {
    expect(channelError("http_403")).toBe("공급자가 거부했습니다(403).");
    expect(channelError("webhook_url_not_allowed")).toContain("https");
    expect(channelError(null)).toBe("");
  });
});
