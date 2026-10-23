import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import {
  EVENT_ACTIVITIES,
  EVENT_CONFIDENCE_LEVELS,
  EVENT_PROVIDERS,
  EVENT_REJECTION_CODES,
  EVENT_SCHEMA_VERSION,
  EVENT_SOURCES,
  EVENT_TYPES,
  type AgentOfficeEventV1,
} from "./index.js";

type JsonSchema = {
  properties: Record<string, { const?: string; enum?: readonly string[] }>;
  required: string[];
  $defs?: Record<string, { properties: Record<string, { enum?: readonly string[] }> }>;
};

const readSchema = (name: string): JsonSchema =>
  JSON.parse(
    readFileSync(new URL(`../schema/${name}`, import.meta.url), "utf8"),
  ) as JsonSchema;

describe("이벤트 계약의 TypeScript 상수", () => {
  it("JSON Schema의 버전과 enum을 그대로 반영한다", () => {
    const schema = readSchema("event-v1.schema.json");

    expect(schema.properties.schema_version.const).toBe(EVENT_SCHEMA_VERSION);
    expect(schema.properties.provider.enum).toEqual(EVENT_PROVIDERS);
    expect(schema.properties.source.enum).toEqual(EVENT_SOURCES);
    expect(schema.properties.event_type.enum).toEqual(EVENT_TYPES);
    expect(schema.properties.activity.enum).toEqual(EVENT_ACTIVITIES);
    expect(schema.properties.confidence.enum).toEqual(EVENT_CONFIDENCE_LEVELS);
  });

  it("필수 필드와 TypeScript 이벤트 구조를 맞춘다", () => {
    const schema = readSchema("event-v1.schema.json");
    const event = {
      schema_version: "1.0",
      event_id: "11111111-1111-4111-8111-111111111111",
      device_id: "22222222-2222-4222-8222-222222222222",
      session_id: "33333333-3333-4333-8333-333333333333",
      agent_id: "44444444-4444-4444-8444-444444444444",
      parent_agent_id: null,
      provider: "codex",
      source: "hook",
      event_type: "tool.started",
      activity: "coding",
      confidence: "observed",
      occurred_at: "2026-10-23T00:00:00Z",
      sequence: 1,
    } satisfies AgentOfficeEventV1;

    expect([...schema.required].sort()).toEqual(Object.keys(event).sort());
  });

  it("배치 거부 코드를 응답 스키마와 맞춘다", () => {
    const schema = readSchema("collector-event-batch-response-v1.schema.json");
    const rejectionCodes = schema.$defs?.rejectedEvent.properties.code.enum;

    expect(rejectionCodes).toEqual(EVENT_REJECTION_CODES);
  });
});
