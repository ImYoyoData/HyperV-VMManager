package hyperv

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const setLocalUserPasswordScript = `
$userName = [string]$opts.userName
$password = [string]$opts.password
$user = Get-LocalUser -Name $userName -ErrorAction SilentlyContinue
if (-not $user) { throw "远程主机上不存在本地账户: $userName" }
$secure = ConvertTo-SecureString -String $password -AsPlainText -Force
Set-LocalUser -Name $userName -Password $secure -ErrorAction Stop
Write-HvResult ([ordered]@{ userName = $userName })
`

// SetLocalUserPassword 通过当前 WinRM 连接修改远程主机本地账户密码。
// 仅用于远程主机；修改本机账户密码应使用 Windows 设置或 net user。
func (c *Client) SetLocalUserPassword(ctx context.Context, req SetLocalUserPasswordRequest) error {
	req.UserName = strings.TrimSpace(req.UserName)
	if req.UserName == "" {
		return errors.New("用户名不能为空")
	}
	if strings.ContainsAny(req.UserName, `\/`) {
		return errors.New("用户名不要带计算机名或域，例如只填 001")
	}
	if err := ValidateName(req.UserName); err != nil {
		return fmt.Errorf("用户名无效: %w", err)
	}
	if req.Password == "" {
		return errors.New("新密码不能为空")
	}
	if strings.ContainsAny(req.Password, "\r\n") {
		return errors.New("密码不能包含换行符")
	}
	return c.run(ctx, setLocalUserPasswordScript, req, nil)
}
