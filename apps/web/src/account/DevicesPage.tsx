import { useCallback, useEffect, useState } from "react";

import { Card, Notice } from "./AccountPages";
import { type SessionInfo, call, messageFor } from "./api";
import { type DeviceSummary, PLATFORM_LABELS, type RegistrationCode, deviceStatus, remaining, sortDevices } from "./devices";

/**
 * 내 PC(#41). 로그인한 사용자가 Collector GUI·CLI에 입력할 단기 일회용 등록 코드를 받고,
 * 등록된 PC를 확인·폐기한다. Collector는 계정 비밀번호를 받지 않는다.
 */
export function DevicesPage() {
  const [session, setSession] = useState<SessionInfo | null | undefined>(undefined);
  const [devices, setDevices] = useState<DeviceSummary[]>([]);
  const [issued, setIssued] = useState<RegistrationCode | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const result = await call<DeviceSummary[]>("GET", "/api/v1/devices");
    setDevices(result.data ?? []);
  }, []);

  useEffect(() => {
    void call<SessionInfo>("GET", "/api/v1/auth/session").then((result) => {
      setSession(result.data);
      if (result.data) {
        void load();
      }
    });
  }, [load]);

  useEffect(() => {
    if (!issued) {
      return undefined;
    }
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [issued]);

  if (session === undefined) {
    return (
      <Card title="내 PC">
        <Notice info="확인 중입니다…" />
      </Card>
    );
  }
  if (session === null) {
    return (
      <Card title="내 PC">
        <p>로그인한 뒤 이 화면에서 Collector에 입력할 PC 등록 코드를 받습니다.</p>
        <p className="account-links">
          <a href="/login">로그인</a>
        </p>
      </Card>
    );
  }

  const issue = async () => {
    setBusy(true);
    setError(null);
    setInfo(null);
    const result = await call<RegistrationCode>("POST", "/api/v1/devices/registration-codes", undefined, session.csrf_token);
    setBusy(false);
    if (result.data) {
      setIssued(result.data);
      setNow(Date.now());
    } else {
      setError(messageFor(result.error));
    }
  };

  const revoke = async (device: DeviceSummary) => {
    if (!window.confirm(`'${device.name}' PC를 폐기합니다. 이 PC의 Collector는 다시 등록해야 합니다. 계속할까요?`)) {
      return;
    }
    const result = await call("POST", `/api/v1/devices/${device.id}/revoke`, undefined, session.csrf_token);
    if (result.ok) {
      setInfo(`'${device.name}' PC를 폐기했습니다.`);
      await load();
    } else {
      setError(messageFor(result.error));
    }
  };

  const left = issued ? remaining(issued.expires_at, now) : null;

  return (
    <>
      <Card title="PC 등록 코드 발급">
        <p>
          Collector 앱의 <strong>기기 등록</strong> 화면(또는 <code>agent-office-collector register --code-stdin</code>)에 아래
          코드를 입력하세요. 코드는 10분 동안 한 번만 쓸 수 있습니다.
        </p>
        <Notice error={error} info={info} />
        {issued && left ? (
          <div className="registration-code" aria-live="polite">
            <output aria-label="등록 코드">{issued.code}</output>
            <span>남은 시간 {left}</span>
          </div>
        ) : issued ? (
          <Notice info="코드가 만료되었습니다. 새 코드를 받으세요." />
        ) : null}
        <button type="button" disabled={busy} onClick={() => void issue()}>
          {issued ? "새 코드 받기" : "PC 등록 코드 발급"}
        </button>
      </Card>
      <Card title="등록된 PC">
        {devices.length === 0 ? (
          <p>등록된 PC가 없습니다.</p>
        ) : (
          <table className="device-table">
            <thead>
              <tr>
                <th>이름</th>
                <th>OS</th>
                <th>Collector</th>
                <th>상태</th>
                <th>마지막 연결</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {sortDevices(devices).map((device) => (
                <tr key={device.id}>
                  <td>{device.name}</td>
                  <td>{PLATFORM_LABELS[device.platform]}</td>
                  <td>{device.collector_version ?? "-"}</td>
                  <td>{deviceStatus(device)}</td>
                  <td>{device.last_seen_at ? new Date(device.last_seen_at).toLocaleString() : "-"}</td>
                  <td>
                    {!device.revoked_at && (
                      <button type="button" className="danger" onClick={() => void revoke(device)}>
                        폐기
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  );
}
