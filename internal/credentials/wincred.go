// Package credentials 使用 Windows 凭据管理器保存远程主机密码。
// 凭据目标直接使用主机地址，因此 VMConnect、Hyper-V 管理器等系统工具
// 也可以在连接同一台主机时复用这份凭据。
package credentials

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"

	"github.com/danieljoos/wincred"
)

// Save 保存远程主机密码。username 建议使用「计算机名\本地用户名」格式，
// 这样 VMConnect 的 Negotiate 认证才能正确使用。
func Save(host, username, password string) error {
	host = strings.TrimSpace(host)
	username = strings.TrimSpace(username)
	if host == "" {
		return errors.New("远程主机地址不能为空")
	}
	if username == "" {
		return errors.New("用户名不能为空")
	}
	credential := wincred.NewGenericCredential(host)
	credential.UserName = username
	credential.CredentialBlob = encodePassword(password)
	credential.Persist = wincred.PersistLocalMachine
	if err := credential.Write(); err != nil {
		return errors.New("写入 Windows 凭据管理器失败: " + err.Error())
	}
	return nil
}

// Load 读取远程主机密码；不存在时返回 ok=false。
func Load(host string) (username string, password string, ok bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", "", false
	}
	credential, err := wincred.GetGenericCredential(host)
	if err != nil || credential == nil {
		return "", "", false
	}
	return credential.UserName, decodePassword(credential.CredentialBlob), true
}

// Saved 判断该主机是否已保存密码。
func Saved(host string) bool {
	_, _, ok := Load(host)
	return ok
}

// Delete 删除远程主机密码。
func Delete(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	credential, err := wincred.GetGenericCredential(host)
	if err != nil || credential == nil {
		return nil
	}
	if err := credential.Delete(); err != nil {
		return errors.New("删除 Windows 凭据失败: " + err.Error())
	}
	return nil
}

// Windows 凭据管理器（cmdkey）通常把密码保存为 UTF-16LE。
// 应用自己写入时也使用 UTF-16LE，方便 VMConnect / Hyper-V 管理器直接复用。
func encodePassword(password string) []byte {
	if password == "" {
		return nil
	}
	units := utf16.Encode([]rune(password))
	buf := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], unit)
	}
	return buf
}

func decodePassword(blob []byte) string {
	if len(blob) == 0 {
		return ""
	}
	if len(blob)%2 == 0 && containsNUL(blob) {
		units := make([]uint16, len(blob)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(blob[i*2:])
		}
		return string(utf16.Decode(units))
	}
	return string(blob)
}

func containsNUL(blob []byte) bool {
	for _, b := range blob {
		if b == 0 {
			return true
		}
	}
	return false
}
