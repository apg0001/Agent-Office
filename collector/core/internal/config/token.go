package config

import (
	"errors"

	"github.com/apg0001/Agent-Office/collector/core/internal/secret"
)

// 장치 토큰 보관 위치(#41).
//
//   - keyring: OS 보안 저장소(서비스 secret.Service, 계정 = 장치 ID). config.json에는 토큰이 없다.
//   - file: OS 보안 저장소를 쓸 수 없을 때의 대체 경로. config.json(0600)에 토큰이 있다.
//
// 저장소는 nil일 수 있다(강제 파일 모드·headless). nil이면 항상 file이다.
const (
	TokenStoreKeyring = "keyring"
	TokenStoreFile    = "file"
)

// ErrTokenUnavailable은 keyring 보관 토큰을 지금 읽을 수 없을 때다(잠김·D-Bus 없음·항목 없음).
var ErrTokenUnavailable = errors.New("config: device token unavailable from os secret store")

func (c Config) storeKind() string {
	if c.TokenStore == TokenStoreKeyring {
		return TokenStoreKeyring
	}
	return TokenStoreFile
}

// InKeyring은 토큰이 OS 보안 저장소에 있는지다.
func (c Config) InKeyring() bool { return c.storeKind() == TokenStoreKeyring }

// Token은 장치 토큰을 돌려준다. keyring이면 저장소에서 읽는다.
func (c Config) Token(st secret.Store) (Secret, error) {
	if !c.InKeyring() {
		return c.DeviceToken, nil
	}
	if st == nil {
		return "", ErrTokenUnavailable
	}
	v, err := st.Get(c.DeviceID)
	if err != nil || v == "" || len(v) > 512 {
		return "", ErrTokenUnavailable
	}
	return Secret(v), nil
}

// SaveWithToken은 c에 token을 붙여 저장한다. 저장소가 있으면 먼저 저장소에 쓰고 다시 읽어 확인한 뒤
// config.json에는 토큰 없이 token_store=keyring만 쓴다. 저장소를 쓸 수 없으면 파일(0600)에 둔다.
// config.json 쓰기가 실패하면 방금 만든 저장소 항목을 지운다. 저장된 Config를 돌려준다.
func SaveWithToken(path string, c Config, token Secret, st secret.Store) (Config, error) {
	c.SchemaVersion = SchemaVersion
	if token == "" || len(token) > 512 {
		return Config{}, ErrInvalid
	}
	if st != nil && putVerified(st, c.DeviceID, token) {
		k := c
		k.TokenStore, k.DeviceToken = TokenStoreKeyring, ""
		if err := Save(path, k); err != nil {
			_ = st.Delete(c.DeviceID)
			return Config{}, err
		}
		return k, nil
	}
	c.TokenStore, c.DeviceToken = TokenStoreFile, token
	if err := Save(path, c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func putVerified(st secret.Store, account string, token Secret) bool {
	if st.Set(account, string(token)) != nil {
		return false
	}
	if v, err := st.Get(account); err != nil || v != string(token) {
		_ = st.Delete(account)
		return false
	}
	return true
}

// MigrateToken은 파일에 있는 토큰을 저장소로 옮긴다(최초 실행·저장소가 나중에 생긴 경우).
// 저장소에 쓰고 다시 읽어 같은지 확인한 다음에만 config.json에서 토큰을 지운다.
// 옮겼으면 새 Config와 true, 저장소가 없거나 실패하면 c 그대로와 false다(파일 보관 유지).
func MigrateToken(path string, c Config, st secret.Store) (Config, bool) {
	if c.InKeyring() || st == nil || c.DeviceToken == "" {
		return c, false
	}
	if !putVerified(st, c.DeviceID, c.DeviceToken) {
		return c, false
	}
	k := c
	k.TokenStore, k.DeviceToken = TokenStoreKeyring, ""
	if err := Save(path, k); err != nil {
		// 파일은 그대로 토큰을 갖고 있다. 저장소 항목은 다음 이전 시도에서 덮어쓴다.
		_ = st.Delete(c.DeviceID)
		return c, false
	}
	return k, true
}

// DeleteToken은 keyring 항목을 지운다. 파일 보관이면 할 일이 없다(config.json 삭제는 호출자).
// 항목이 이미 없으면 nil이다.
func DeleteToken(c Config, st secret.Store) error {
	if !c.InKeyring() {
		return nil
	}
	if st == nil {
		return ErrTokenUnavailable
	}
	if err := st.Delete(c.DeviceID); err != nil && !errors.Is(err, secret.ErrNotFound) {
		return ErrTokenUnavailable
	}
	return nil
}
