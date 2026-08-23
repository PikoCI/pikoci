//go:build integration

package selenium_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tebeka/selenium"
)

func TestOAuthLoginWithKeycloak(t *testing.T) {
	if os.Getenv("KEYCLOAK_URL") == "" {
		t.Fatal("KEYCLOAK_URL env var is required for OAuth integration tests")
	}

	wd := getRemote(t)
	wd.ResizeWindow("", 1500, 1500)

	err := wd.Get(pikoURL)
	require.NoError(t, err)

	// Wait for the login page with Keycloak button
	waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Log In"), 10*time.Second)
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		els, err := wd.FindElements(selenium.ByCSSSelector, "button.btn-outline-secondary")
		if err != nil || len(els) == 0 {
			return false
		}
		for _, el := range els {
			txt, _ := el.Text()
			if txt == "Log in with Keycloak" {
				return true
			}
		}
		return false
	}, 10*time.Second)

	// Click Keycloak button
	btns, err := wd.FindElements(selenium.ByCSSSelector, "button.btn-outline-secondary")
	require.NoError(t, err)
	for _, btn := range btns {
		txt, _ := btn.Text()
		if txt == "Log in with Keycloak" {
			require.NoError(t, btn.Click())
			break
		}
	}

	// Wait for Keycloak login page
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		el, err := wd.FindElement(selenium.ByCSSSelector, "#username")
		return err == nil && el != nil
	}, 15*time.Second)

	// Enter credentials
	kcUser, err := wd.FindElement(selenium.ByCSSSelector, "#username")
	require.NoError(t, err)
	kcPass, err := wd.FindElement(selenium.ByCSSSelector, "#password")
	require.NoError(t, err)
	kcUser.SendKeys("testuser")
	kcPass.SendKeys("testpassword")

	// Submit Keycloak login
	loginBtn, err := wd.FindElement(selenium.ByCSSSelector, "#kc-login")
	if err != nil {
		loginBtn, err = wd.FindElement(selenium.ByCSSSelector, "input[type=submit]")
		require.NoError(t, err)
	}
	require.NoError(t, loginBtn.Click())

	// Wait for profile completion page (OAuth round-trip can be slow on arm64)
	waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Complete Your Profile"), 30*time.Second)

	// Username should be pre-filled
	usernameField, err := wd.FindElement(selenium.ByCSSSelector, "#username")
	require.NoError(t, err)
	val, _ := usernameField.GetAttribute("value")
	if val == "" {
		usernameField.SendKeys("testuser")
	}

	// Submit profile
	submitBtn, err := wd.FindElement(selenium.ByCSSSelector, "button[type=submit]")
	require.NoError(t, err)
	require.NoError(t, submitBtn.Click())

	// Wait for Teams page — allow extra time for the POST + redirect
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		curURL, _ := wd.CurrentURL()
		el, err := wd.FindElement(selenium.ByCSSSelector, "#breadcrumb")
		if err != nil {
			h1, _ := wd.FindElement(selenium.ByCSSSelector, "h1")
			h1txt := ""
			if h1 != nil {
				h1txt, _ = h1.Text()
			}
			t.Logf("waiting for Teams: url=%s h1=%s", curURL, h1txt)
			return false
		}
		txt, _ := el.Text()
		return txt == "Teams"
	}, 15*time.Second)

	t.Log("New user OAuth login successful")

	// --- Returning user flow ---

	// Logout via nav dropdown
	time.Sleep(time.Second)
	navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
	require.NoError(t, err)
	require.NoError(t, navLink.Click())
	time.Sleep(500 * time.Millisecond)

	logoutBtn, err := wd.FindElement(selenium.ByCSSSelector, "#logout")
	require.NoError(t, err)
	require.NoError(t, logoutBtn.Click())

	// Navigate to login page explicitly
	time.Sleep(time.Second)
	err = wd.Get(pikoURL + "/login")
	require.NoError(t, err)

	waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Log In"), 10*time.Second)

	// Click Keycloak again
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		els, _ := wd.FindElements(selenium.ByCSSSelector, "button.btn-outline-secondary")
		for _, el := range els {
			txt, _ := el.Text()
			if txt == "Log in with Keycloak" {
				return true
			}
		}
		return false
	}, 10*time.Second)

	btns, _ = wd.FindElements(selenium.ByCSSSelector, "button.btn-outline-secondary")
	for _, btn := range btns {
		txt, _ := btn.Text()
		if txt == "Log in with Keycloak" {
			btn.Click()
			break
		}
	}

	// Keycloak may have SSO session — either goes straight through or shows login
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		// Check Teams page (SSO session active, user already linked)
		el, err := wd.FindElement(selenium.ByCSSSelector, "#breadcrumb")
		if err == nil {
			txt, _ := el.Text()
			if txt == "Teams" {
				return true
			}
		}
		// Or Keycloak login page
		el, err = wd.FindElement(selenium.ByCSSSelector, "#kc-login")
		return err == nil && el != nil
	}, 15*time.Second)

	// If on Keycloak, authenticate again
	if kcUser, err := wd.FindElement(selenium.ByCSSSelector, "#kc-login"); err == nil && kcUser != nil {
		u, _ := wd.FindElement(selenium.ByCSSSelector, "#username")
		p, _ := wd.FindElement(selenium.ByCSSSelector, "#password")
		if u != nil {
			u.SendKeys("testuser")
		}
		if p != nil {
			p.SendKeys("testpassword")
		}
		kcUser.Click()
	}

	// Should land on Teams without profile completion
	waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 15*time.Second)
	t.Log("Returning user OAuth login successful")

	// --- Password flow for OAuth user ---

	// Navigate to profile password tab
	err = wd.Get(pikoURL + "/profile?tab=password")
	require.NoError(t, err)
	waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Profile"), 10*time.Second)

	// Click the Password tab
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		btns, _ := wd.FindElements(selenium.ByCSSSelector, ".nav-link")
		for _, b := range btns {
			txt, _ := b.Text()
			if txt == "Password" {
				b.Click()
				return true
			}
		}
		return false
	}, 5*time.Second)
	time.Sleep(500 * time.Millisecond)

	// OAuth user with no password: should NOT see current password field
	// but SHOULD see the info banner
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		el, err := wd.FindElement(selenium.ByCSSSelector, ".alert-info")
		return err == nil && el != nil
	}, 5*time.Second)
	_, err = wd.FindElement(selenium.ByCSSSelector, "#current_password")
	require.Error(t, err, "current_password field should be hidden for OAuth user without password")
	t.Log("OAuth user without password: current password field hidden")

	// Set a new password
	newPass, err := wd.FindElement(selenium.ByCSSSelector, "#new_password")
	require.NoError(t, err)
	newPass.SendKeys("localpass123")

	confirmPass, err := wd.FindElement(selenium.ByCSSSelector, "#confirm_password")
	require.NoError(t, err)
	confirmPass.SendKeys("localpass123")

	changeBtn, err := wd.FindElement(selenium.ByCSSSelector, "#change-password-form button[type=submit]")
	require.NoError(t, err)
	require.NoError(t, changeBtn.Click())

	// Wait for success toast
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		els, _ := wd.FindElements(selenium.ByCSSSelector, ".piko-toast")
		for _, el := range els {
			txt, _ := el.Text()
			if txt == "Password changed successfully" {
				return true
			}
		}
		return false
	}, 10*time.Second)
	t.Log("OAuth user set first password successfully")

	// After setting password, current password field should now appear
	time.Sleep(time.Second)
	waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
		el, err := wd.FindElement(selenium.ByCSSSelector, "#current_password")
		return err == nil && el != nil
	}, 5*time.Second)

	// Info banner should be gone
	_, err = wd.FindElement(selenium.ByCSSSelector, ".alert-info")
	require.Error(t, err, "info banner should be hidden after password is set")
	t.Log("After password set: current password field visible, info banner gone")

	// Local password login is verified via unit tests
	// (TestChangePassword_OAuthUserFirstPassword + direct Go integration test)
}
