import { SHARE_MODE_HELP, SHARE_VISIBLE_TEXT } from "./teams";

/**
 * 팀 만들기·초대 수락 화면의 공유 동의(#197). "모두 공유"가 미리 체크되어 있고, 팀원에게 보이는 것과
 * 보이지 않는 것을 함께 보여 준다. 체크를 끄면 "선택한 프로젝트만"으로 시작한다.
 */
export function ShareConsent({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <div className="share-consent">
      <label>
        <input type="checkbox" name="share_all" checked={checked} onChange={(event) => onChange(event.target.checked)} /> 이 팀에 내 작업을
        모두 공유
      </label>
      <p className="hint">{SHARE_VISIBLE_TEXT}</p>
      <p className="hint">
        {checked ? SHARE_MODE_HELP.all : "선택한 프로젝트만 공유합니다. 팀 화면에서 공유할 프로젝트를 고르세요."} 언제든 팀 화면에서 바꿀
        수 있습니다.
      </p>
    </div>
  );
}
