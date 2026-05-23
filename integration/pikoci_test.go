//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tebeka/selenium"
	thttp "github.com/xescugc/pikoci/pikoci/transport/http"
)

func TestPikoCI(t *testing.T) {
	wd := getRemote(t)
	// Some cases some elements are not on the viewport, to avoid
	// some weird logic to scroll to them I just resize the widnow
	wd.ResizeWindow("", 1500, 1500)

	err := wd.Get(pikoURL)
	require.NoError(t, err)

	t.Run("Admin", func(t *testing.T) {
		t.Run("Login", func(t *testing.T) {
			title, err := wd.FindElement(selenium.ByCSSSelector, "h1")
			require.NoError(t, err)

			txt, err := title.Text()
			require.NoError(t, err)
			require.Equal(t, "Log In", txt)

			username, err := wd.FindElement(selenium.ByCSSSelector, "#username")
			require.NoError(t, err)
			password, err := wd.FindElement(selenium.ByCSSSelector, "#password")
			require.NoError(t, err)

			username.SendKeys("admin")
			password.SendKeys("admin123")

			login, err := wd.FindElement(selenium.ByCSSSelector, "#login")
			require.NoError(t, err)

			err = login.Click()
			require.NoError(t, err)

			// Default admin123 triggers forced password change — handle it
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Profile"), 10*time.Second)

			curPass, err := wd.FindElement(selenium.ByCSSSelector, "#current_password")
			require.NoError(t, err)
			curPass.SendKeys("admin123")

			newPass, err := wd.FindElement(selenium.ByCSSSelector, "#new_password")
			require.NoError(t, err)
			newPass.SendKeys("newadmin123")

			confirmPass, err := wd.FindElement(selenium.ByCSSSelector, "#confirm_password")
			require.NoError(t, err)
			confirmPass.SendKeys("newadmin123")

			changeBtn, err := wd.FindElement(selenium.ByCSSSelector, "#change-password-form button[type=submit]")
			require.NoError(t, err)
			err = changeBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, ".alert-success", "Password changed successfully"), 15*time.Second)

			// Navigate to teams
			wd.Get(pikoURL + "/")
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)
		})

		t.Run("New Team", func(t *testing.T) {
			teams, err := wd.FindElements(selenium.ByCSSSelector, ".piko-team-row")
			require.NoError(t, err)
			require.Equal(t, 1, len(teams))

			ntBtn, err := wd.FindElement(selenium.ByCSSSelector, "#team-new")
			require.NoError(t, err)

			err = ntBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "New Team"), 5*time.Second)

			tNameI, err := wd.FindElement(selenium.ByCSSSelector, "#name")
			require.NoError(t, err)

			tNameI.SendKeys("My New Team")
			ctBtn, err := wd.FindElement(selenium.ByCSSSelector, "form>button")
			require.NoError(t, err)

			err = ctBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMy New Team"), 5*time.Second)
		})
		t.Run("Update Team", func(t *testing.T) {
			logo, err := wd.FindElement(selenium.ByCSSSelector, "#logo")
			require.NoError(t, err)

			err = logo.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)

			teams, err := wd.FindElements(selenium.ByCSSSelector, ".piko-team-row")
			require.NoError(t, err)
			require.Equal(t, 2, len(teams))

			manages, err := wd.FindElements(selenium.ByCSSSelector, "#manage")
			require.NoError(t, err)
			require.Equal(t, 2, len(manages))

			err = manages[1].Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMy New Team"), 5*time.Second)

			nameInput, err := wd.FindElement(selenium.ByCSSSelector, "#name")
			require.NoError(t, err)

			nameInput.Clear()
			nameInput.SendKeys("My New Updated Team")

			utBtn, err := wd.FindElement(selenium.ByCSSSelector, "form>button")
			require.NoError(t, err)

			err = utBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMy New Updated Team"), 5*time.Second)
		})
		t.Run("Add Member", func(t *testing.T) {
			members, err := wd.FindElements(selenium.ByCSSSelector, "tbody>tr")
			require.NoError(t, err)
			require.Equal(t, 1, len(members))

			nmBtn, err := wd.FindElement(selenium.ByCSSSelector, "#new-member")
			require.NoError(t, err)

			err = nmBtn.Click()
			require.NoError(t, err)

			// We check that no more are added if one is open
			err = nmBtn.Click()
			require.NoError(t, err)

			err = nmBtn.Click()
			require.NoError(t, err)

			// As we fetch the users to fill in we have to wait
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				opts, err := wd.FindElements(selenium.ByCSSSelector, "option")
				require.NoError(t, err)

				return 2 == len(opts)
			}, 5*time.Second)

			members, err = wd.FindElements(selenium.ByCSSSelector, "tbody>tr")
			require.NoError(t, err)
			require.Equal(t, 2, len(members))

			cmBtn, err := wd.FindElement(selenium.ByCSSSelector, "#create")
			require.NoError(t, err)

			err = cmBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				dBtns, err := wd.FindElements(selenium.ByCSSSelector, "#delete")
				require.NoError(t, err)

				return 2 == len(dBtns)
			}, 5*time.Second)

			members, err = wd.FindElements(selenium.ByCSSSelector, "tbody>tr")
			require.NoError(t, err)
			require.Equal(t, 2, len(members))
		})
		t.Run("Update Member", func(t *testing.T) {
			aBtns, err := wd.FindElements(selenium.ByCSSSelector, "#admin")
			require.NoError(t, err)
			require.Equal(t, 2, len(aBtns))

			as1, err := aBtns[0].IsSelected()
			require.NoError(t, err)
			ae1, err := aBtns[0].IsEnabled()
			require.NoError(t, err)
			as2, err := aBtns[1].IsSelected()
			require.NoError(t, err)
			ae2, err := aBtns[1].IsEnabled()
			require.NoError(t, err)
			require.True(t, as1)
			require.True(t, ae1)
			require.False(t, as2)
			require.True(t, ae2)

			err = aBtns[1].Click()
			require.NoError(t, err)

			aBtns, err = wd.FindElements(selenium.ByCSSSelector, "#admin")
			require.NoError(t, err)

			as1, err = aBtns[0].IsSelected()
			require.NoError(t, err)
			as2, err = aBtns[1].IsSelected()
			require.NoError(t, err)
			require.True(t, as1)
			require.True(t, as2)
		})
		t.Run("Delete Member", func(t *testing.T) {
			members, err := wd.FindElements(selenium.ByCSSSelector, "tbody>tr")
			require.NoError(t, err)
			require.Equal(t, 2, len(members))

			dBtns, err := wd.FindElements(selenium.ByCSSSelector, "#delete")
			require.NoError(t, err)
			require.Equal(t, 2, len(dBtns))

			err = dBtns[1].Click()
			require.NoError(t, err)

			members, err = wd.FindElements(selenium.ByCSSSelector, "tbody>tr")
			require.NoError(t, err)
			require.Equal(t, 1, len(members))
		})
		t.Run("Delete Team", func(t *testing.T) {
			tmsBtn, err := wd.FindElement(selenium.ByLinkText, "Teams")
			require.NoError(t, err)

			err = tmsBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)

			teams, err := wd.FindElements(selenium.ByCSSSelector, ".piko-team-row")
			require.NoError(t, err)
			require.Equal(t, 2, len(teams))

			dBtns, err := wd.FindElements(selenium.ByCSSSelector, "#delete")
			require.NoError(t, err)
			require.Equal(t, 2, len(dBtns))

			err = dBtns[1].Click()
			require.NoError(t, err)

			teams, err = wd.FindElements(selenium.ByCSSSelector, ".piko-team-row")
			require.NoError(t, err)
			require.Equal(t, 1, len(teams))
		})
		t.Run("Pipelines", func(t *testing.T) {
			pipelines, err := wd.FindElement(selenium.ByCSSSelector, "#pipelines")
			require.NoError(t, err)

			err = pipelines.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)
		})
		t.Run("New Pipeline", func(t *testing.T) {
			npp, err := wd.FindElement(selenium.ByCSSSelector, "#pipelines-new")
			require.NoError(t, err)

			err = npp.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "New Pipeline"), 5*time.Second)

			name, err := wd.FindElement(selenium.ByCSSSelector, "#name")
			require.NoError(t, err)

			setEditorContent(t, wd, `
resource "cron" "my_cron" {
  check_interval = "@every 1m"
}

job "gen" {
  get "cron" "my_cron" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["IN"]
    }
  }
}`)
			name.SendKeys("cron")

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "#graph svg")

				return err == nil
			}, 5*time.Second)

			cpBtn, err := wd.FindElement(selenium.ByCSSSelector, "form button[type='submit']")
			require.NoError(t, err)

			err = cpBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)
		})
		t.Run("Edit Pipeline", func(t *testing.T) {
			epBtn, err := wd.FindElement(selenium.ByCSSSelector, "#edit-pipeline")
			require.NoError(t, err)

			err = epBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Update Pipeline"), 5*time.Second)

			setEditorContent(t, wd, `
resource "cron" "my_cron_edit" {
  check_interval = "@every 1m"
}

job "gen" {
  get "cron" "my_cron_edit" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["IN"]
    }
  }
}`)

			upBtn, err := wd.FindElement(selenium.ByCSSSelector, "form button[type='submit']")
			require.NoError(t, err)

			err = upBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#a_node1", "cron.my_cron_edit"), 5*time.Second)
		})
		t.Run("Resource Versions", func(t *testing.T) {
			// TODO: Find a way to click the PP SVG
			res, err := wd.FindElement(selenium.ByCSSSelector, "#a_node1>a")
			require.NoError(t, err)

			url, err := res.GetAttribute("xlink:href")
			require.NoError(t, err)

			//spew.Dump(res)
			//screenshot(t, wd)
			//err = res.MoveTo(0, 0)
			//require.NoError(t, err)
			//wd.Click(selenium.LeftButton)
			//err = res.Click()
			//require.NoError(t, err)

			err = wd.Get(pikoURL + url)
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron\nResources\ncron.my_cron_edit\nVersions"), 5*time.Second)

			rvs, err := wd.FindElements(selenium.ByCSSSelector, "#resource-versions>div")
			require.NoError(t, err)
			require.Equal(t, 0, len(rvs))

			tgBtn, err := wd.FindElement(selenium.ByCSSSelector, "#trigger-resource")
			require.NoError(t, err)

			err = tgBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				rvs, err := wd.FindElements(selenium.ByCSSSelector, "#resource-versions>div")
				require.NoError(t, err)

				return len(rvs) > 0
			}, 5*time.Second)
		})
		t.Run("Job Builds", func(t *testing.T) {
			ppBtn, err := wd.FindElement(selenium.ByLinkText, "cron")
			require.NoError(t, err)

			err = ppBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")

				return err == nil
			}, 5*time.Second)

			res, err := wd.FindElement(selenium.ByCSSSelector, "#a_node2>a")
			require.NoError(t, err)

			url, err := res.GetAttribute("xlink:href")
			require.NoError(t, err)

			err = wd.Get(pikoURL + url)
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron\nJobs\ngen\nBuilds"), 5*time.Second)

			builds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
			require.NoError(t, err)
			require.Equal(t, 1, len(builds))

			tjBtn, err := wd.FindElement(selenium.ByCSSSelector, "#trigger-job")
			require.NoError(t, err)

			err = tjBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				builds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
				require.NoError(t, err)

				return len(builds) == 2
			}, 5*time.Second)

		})
		t.Run("Pipeline Card Last Build Timestamp", func(t *testing.T) {
			// Navigate to pipelines list
			ppsBtn, err := wd.FindElement(selenium.ByLinkText, "Pipelines")
			require.NoError(t, err)

			err = ppsBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)

			// Wait for the card status to include a relative timestamp
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-card-status")
				if err != nil {
					return false
				}
				txt, err := el.Text()
				if err != nil {
					return false
				}
				// After builds have run, the card footer should contain "ago"
				return containsString(txt, "ago") || containsString(txt, "just now")
			}, 10*time.Second)
		})
		t.Run("Delete Pipeline", func(t *testing.T) {
			ppBtn, err := wd.FindElement(selenium.ByCSSSelector, ".card")
			require.NoError(t, err)

			err = ppBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)

			dpBtn, err := wd.FindElement(selenium.ByCSSSelector, "#delete-pipeline")
			require.NoError(t, err)

			err = dpBtn.Click()
			require.NoError(t, err)

			err = wd.AcceptAlert()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)
		})
		t.Run("Prepare for Pepito", func(t *testing.T) {
			t.Run("Create Pipeline", func(t *testing.T) {
				npp, err := wd.FindElement(selenium.ByCSSSelector, "#pipelines-new")
				require.NoError(t, err)

				err = npp.Click()
				require.NoError(t, err)

				waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "New Pipeline"), 5*time.Second)

				name, err := wd.FindElement(selenium.ByCSSSelector, "#name")
				require.NoError(t, err)

				setEditorContent(t, wd, `
resource "cron" "my_cron" {
  check_interval = "@every 1m"
}

job "gen" {
  get "cron" "my_cron" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["IN"]
    }
  }
}`)
				name.SendKeys("cron")

				waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
					_, err := wd.FindElement(selenium.ByCSSSelector, "#graph svg")

					return err == nil
				}, 5*time.Second)

				cpBtn, err := wd.FindElement(selenium.ByCSSSelector, "form button[type='submit']")
				require.NoError(t, err)

				err = cpBtn.Click()
				require.NoError(t, err)

				waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)

				waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
					_, err := wd.FindElement(selenium.ByCSSSelector, "#graphviz svg")

					return err == nil
				}, 5*time.Second)

				node, err := wd.FindElement(selenium.ByCSSSelector, "#a_node1")
				require.NoError(t, err)

				txt, err := node.Text()
				require.NoError(t, err)
				require.Equal(t, "cron.my_cron", txt)
			})
			t.Run("Add Pepito to Team", func(t *testing.T) {
				mtBtn, err := wd.FindElement(selenium.ByLinkText, "Main")
				require.NoError(t, err)

				err = mtBtn.Click()
				require.NoError(t, err)

				nmBtn, err := wd.FindElement(selenium.ByCSSSelector, "#new-member")
				require.NoError(t, err)

				err = nmBtn.Click()
				require.NoError(t, err)

				// We check that no more are added if one is open
				err = nmBtn.Click()
				require.NoError(t, err)

				err = nmBtn.Click()
				require.NoError(t, err)

				// As we fetch the users to fill in we have to wait
				waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
					opts, err := wd.FindElements(selenium.ByCSSSelector, "option")
					require.NoError(t, err)

					return 2 == len(opts)
				}, 5*time.Second)

				members, err := wd.FindElements(selenium.ByCSSSelector, "tbody>tr")
				require.NoError(t, err)
				require.Equal(t, 2, len(members))

				cmBtn, err := wd.FindElement(selenium.ByCSSSelector, "#create")
				require.NoError(t, err)

				err = cmBtn.Click()
				require.NoError(t, err)

				waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
					dBtns, err := wd.FindElements(selenium.ByCSSSelector, "#delete")
					require.NoError(t, err)

					return 2 == len(dBtns)
				}, 5*time.Second)

				members, err = wd.FindElements(selenium.ByCSSSelector, "tbody>tr")
				require.NoError(t, err)
				require.Equal(t, 2, len(members))
			})
		})
		t.Run("Logout", func(t *testing.T) {
			navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
			require.NoError(t, err)

			err = navLink.Click()
			require.NoError(t, err)

			logoutBtn, err := wd.FindElement(selenium.ByCSSSelector, "#logout")
			require.NoError(t, err)

			err = logoutBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Log In"), 5*time.Second)

		})
	})
	t.Run("Member", func(t *testing.T) {
		t.Run("Log In", func(t *testing.T) {
			username, err := wd.FindElement(selenium.ByCSSSelector, "#username")
			require.NoError(t, err)
			password, err := wd.FindElement(selenium.ByCSSSelector, "#password")
			require.NoError(t, err)

			username.SendKeys("pepito")
			password.SendKeys("pepito")

			login, err := wd.FindElement(selenium.ByCSSSelector, "#login")
			require.NoError(t, err)

			err = login.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)
		})
		t.Run("Teams", func(t *testing.T) {
			teams, err := wd.FindElements(selenium.ByCSSSelector, ".piko-team-row")
			require.NoError(t, err)
			require.Equal(t, 1, len(teams))

			_, err = wd.FindElement(selenium.ByCSSSelector, "#team-new")
			require.Error(t, err)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#delete")
			require.Error(t, err)
		})
		t.Run("Navigate to New Team redirects", func(t *testing.T) {
			err := wd.Get(pikoURL + "/teams/new")
			require.NoError(t, err)

			// Should be redirected back to teams list
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)

			// Should not see the new team form
			_, err = wd.FindElement(selenium.ByCSSSelector, "form>button")
			require.Error(t, err)
		})
		t.Run("Manage Team", func(t *testing.T) {
			manages, err := wd.FindElements(selenium.ByCSSSelector, "#manage")
			require.NoError(t, err)
			require.Equal(t, 1, len(manages))

			err = manages[0].Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain"), 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, "form>button")
			require.Error(t, err)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#new-member")
			require.Error(t, err)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#delete")
			require.Error(t, err)

			// Name input should be disabled for members
			nameInput, err := wd.FindElement(selenium.ByCSSSelector, "#name")
			require.NoError(t, err)
			nameEnabled, err := nameInput.IsEnabled()
			require.NoError(t, err)
			require.False(t, nameEnabled)

			aBtns, err := wd.FindElements(selenium.ByCSSSelector, "#admin")
			require.NoError(t, err)
			require.Equal(t, 2, len(aBtns))

			as1, err := aBtns[0].IsSelected()
			require.NoError(t, err)
			ae1, err := aBtns[0].IsEnabled()
			require.NoError(t, err)
			as2, err := aBtns[1].IsSelected()
			require.NoError(t, err)
			ae2, err := aBtns[1].IsEnabled()
			require.NoError(t, err)
			require.True(t, as1)
			require.False(t, ae1)
			require.False(t, as2)
			require.False(t, ae2)
		})
		t.Run("Pipelines", func(t *testing.T) {
			tmsBtn, err := wd.FindElement(selenium.ByLinkText, "Teams")
			require.NoError(t, err)

			err = tmsBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)

			pipelines, err := wd.FindElement(selenium.ByCSSSelector, "#pipelines")
			require.NoError(t, err)

			err = pipelines.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#pipelines-new")
			require.Error(t, err)
		})
		t.Run("Navigate to New Pipeline redirects", func(t *testing.T) {
			err := wd.Get(pikoURL + "/teams/main/pipelines/new")
			require.NoError(t, err)

			// Should be redirected back to pipelines list
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)
		})
		t.Run("Pipeline", func(t *testing.T) {
			ppBtn, err := wd.FindElement(selenium.ByCSSSelector, ".card")
			require.NoError(t, err)

			err = ppBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#edit-pipeline")
			require.Error(t, err)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#delete-pipeline")
			require.Error(t, err)
		})
		t.Run("Navigate to Edit Pipeline redirects", func(t *testing.T) {
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron/edit")
			require.NoError(t, err)

			// Should be redirected back to pipeline show page
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#edit-pipeline")
			require.Error(t, err)
		})
		t.Run("Resource Versions", func(t *testing.T) {
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")

				return err == nil
			}, 5*time.Second)

			res, err := wd.FindElement(selenium.ByCSSSelector, "#a_node1>a")
			require.NoError(t, err)

			url, err := res.GetAttribute("xlink:href")
			require.NoError(t, err)

			err = wd.Get(pikoURL + url)
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron\nResources\ncron.my_cron\nVersions"), 5*time.Second)

			// Members can trigger resources
			tgBtn, err := wd.FindElement(selenium.ByCSSSelector, "#trigger-resource")
			require.NoError(t, err)

			err = tgBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				rvs, err := wd.FindElements(selenium.ByCSSSelector, "#resource-versions>div")
				require.NoError(t, err)

				return len(rvs) > 0
			}, 5*time.Second)
		})
		t.Run("Job Builds", func(t *testing.T) {
			ppBtn, err := wd.FindElement(selenium.ByLinkText, "cron")
			require.NoError(t, err)

			err = ppBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")

				return err == nil
			}, 5*time.Second)

			res, err := wd.FindElement(selenium.ByCSSSelector, "#a_node2>a")
			require.NoError(t, err)

			url, err := res.GetAttribute("xlink:href")
			require.NoError(t, err)

			err = wd.Get(pikoURL + url)
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron\nJobs\ngen\nBuilds"), 5*time.Second)

			// Members can trigger jobs
			initialBuilds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
			require.NoError(t, err)
			initialCount := len(initialBuilds)

			tjBtn, err := wd.FindElement(selenium.ByCSSSelector, "#trigger-job")
			require.NoError(t, err)

			err = tjBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				builds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
				require.NoError(t, err)

				return len(builds) > initialCount
			}, 5*time.Second)
		})
	})
	t.Run("PublicPipeline", func(t *testing.T) {
		// Pepito is logged in. Log out first.
		navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
		require.NoError(t, err)
		err = navLink.Click()
		require.NoError(t, err)
		logoutBtn, err := wd.FindElement(selenium.ByCSSSelector, "#logout")
		require.NoError(t, err)
		err = logoutBtn.Click()
		require.NoError(t, err)
		waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Log In"), 5*time.Second)

		// Set pipeline public via admin HTTP API
		loginBody, _ := json.Marshal(thttp.UserLoginRequest{
			Username: "admin",
			Password: "newadmin123",
		})
		loginReq, err := http.NewRequest(http.MethodPost, pikoURL+"/login.json", bytes.NewReader(loginBody))
		require.NoError(t, err)
		loginResp, err := http.DefaultClient.Do(loginReq)
		require.NoError(t, err)
		defer loginResp.Body.Close()
		require.Equal(t, http.StatusOK, loginResp.StatusCode)
		var lr thttp.UserLoginResponse
		json.NewDecoder(loginResp.Body).Decode(&lr)
		require.Empty(t, lr.Err)
		adminJWT := lr.Data.JWT

		pub := true
		updateBody, _ := json.Marshal(struct {
			Public *bool `json:"public"`
		}{Public: &pub})
		updateReq, err := http.NewRequest(http.MethodPut, pikoURL+"/teams/main/pipelines/cron.json", bytes.NewReader(updateBody))
		require.NoError(t, err)
		updateReq.Header.Set("Authorization", "Bearer "+adminJWT)
		updateResp, err := http.DefaultClient.Do(updateReq)
		require.NoError(t, err)
		defer updateResp.Body.Close()
		var updateResult thttp.UpdatePipelineResponse
		json.NewDecoder(updateResp.Body).Decode(&updateResult)
		require.Empty(t, updateResult.Err, "update pipeline error")
		require.Equal(t, http.StatusOK, updateResp.StatusCode)

		t.Run("CanViewPipeline", func(t *testing.T) {
			// Navigate to public pipeline without being logged in
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron")
			require.NoError(t, err)

			// Pipeline graph should be visible
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")
				return err == nil
			}, 5*time.Second)
		})

		t.Run("CanViewJobBuilds", func(t *testing.T) {
			res, err := wd.FindElement(selenium.ByCSSSelector, "#a_node2>a")
			require.NoError(t, err)

			url, err := res.GetAttribute("xlink:href")
			require.NoError(t, err)

			err = wd.Get(pikoURL + url)
			require.NoError(t, err)

			// Builds should be visible
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				builds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
				if err != nil {
					return false
				}
				return len(builds) > 0
			}, 5*time.Second)
		})

		t.Run("NoRetryButton", func(t *testing.T) {
			// Retry button should not be present for public viewers
			_, err := wd.FindElement(selenium.ByCSSSelector, ".piko-retry-build")
			require.Error(t, err, "retry button should not be visible on public pipeline view")
		})

		t.Run("NoCancelButton", func(t *testing.T) {
			// Cancel button should not be present for public viewers
			_, err := wd.FindElement(selenium.ByCSSSelector, ".piko-cancel-build")
			require.Error(t, err, "cancel button should not be visible on public pipeline view")
		})

		t.Run("NoTriggerJobButton", func(t *testing.T) {
			// Trigger job button should not be present for public viewers
			_, err := wd.FindElement(selenium.ByCSSSelector, "#trigger-job")
			require.Error(t, err, "trigger job button should not be visible on public pipeline view")
		})

		// Log back in as pepito for the RefreshToken test
		err = wd.Get(pikoURL)
		require.NoError(t, err)
		waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Log In"), 5*time.Second)

		username, err := wd.FindElement(selenium.ByCSSSelector, "#username")
		require.NoError(t, err)
		password, err := wd.FindElement(selenium.ByCSSSelector, "#password")
		require.NoError(t, err)
		username.SendKeys("pepito")
		password.SendKeys("pepito")
		login, err := wd.FindElement(selenium.ByCSSSelector, "#login")
		require.NoError(t, err)
		err = login.Click()
		require.NoError(t, err)
		waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)
	})
	t.Run("RefreshToken", func(t *testing.T) {
		// Pepito is still logged in as a non-admin member of "main".
		// We promote pepito to team admin via a direct HTTP call (as admin),
		// then navigate in the browser. The next Backbone.sync fetch should
		// detect the stale JWT via the X-Refresh-Token header, auto-refresh
		// the session, and pepito should now see admin controls.

		// Step 1: Get admin JWT via HTTP
		loginBody, _ := json.Marshal(thttp.UserLoginRequest{
			Username: "admin",
			Password: "newadmin123",
		})
		loginReq, err := http.NewRequest(http.MethodPost, pikoURL+"/login.json", bytes.NewReader(loginBody))
		require.NoError(t, err)
		loginResp, err := http.DefaultClient.Do(loginReq)
		require.NoError(t, err)
		defer loginResp.Body.Close()
		require.Equal(t, http.StatusOK, loginResp.StatusCode)
		var lr thttp.UserLoginResponse
		json.NewDecoder(loginResp.Body).Decode(&lr)
		require.Empty(t, lr.Err)
		adminJWT := lr.Data.JWT

		// Step 2: Promote pepito to admin on "main" team via HTTP
		updateBody, _ := json.Marshal(thttp.UpdateTeamMemberRequest{Admin: true})
		updateReq, err := http.NewRequest(http.MethodPut, pikoURL+"/teams/main/members/pepito.json", bytes.NewReader(updateBody))
		require.NoError(t, err)
		updateReq.Header.Set("Authorization", "Bearer "+adminJWT)
		updateResp, err := http.DefaultClient.Do(updateReq)
		require.NoError(t, err)
		defer updateResp.Body.Close()
		require.Equal(t, http.StatusOK, updateResp.StatusCode)

		// Step 3: Verify the server returns X-Refresh-Token for pepito's stale JWT.
		// Get pepito's JWT from the browser's localStorage.
		pepitoJWT, err := wd.ExecuteScript("return JSON.parse(localStorage.getItem('piko-user-jwt')).jwt", nil)
		require.NoError(t, err)
		require.NotNil(t, pepitoJWT)

		// Verify the header is returned
		checkReq, err := http.NewRequest(http.MethodGet, pikoURL+"/teams.json", nil)
		require.NoError(t, err)
		checkReq.Header.Set("Authorization", "Bearer "+pepitoJWT.(string))
		checkResp, err := http.DefaultClient.Do(checkReq)
		require.NoError(t, err)
		defer checkResp.Body.Close()
		require.Equal(t, "true", checkResp.Header.Get("X-Refresh-Token"), "server should signal stale JWT")

		// Step 4: Navigate to teams list to trigger the Backbone.sync fetch,
		// which detects the stale JWT and fires the async refresh.
		err = wd.Get(pikoURL + "/")
		require.NoError(t, err)
		waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)

		// Wait for async refresh to complete and save to localStorage
		time.Sleep(5 * time.Second)

		// Step 5: Full page reload to pipelines — reads refreshed session
		err = wd.Get(pikoURL + "/teams/main/pipelines")
		require.NoError(t, err)
		waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)

		// Step 6: Check for admin button — may need one more reload
		waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
			_, err := wd.FindElement(selenium.ByCSSSelector, "#pipelines-new")
			if err == nil {
				return true
			}
			// Reload and try again
			wd.Get(pikoURL + "/teams/main/pipelines")
			time.Sleep(2 * time.Second)
			_, err = wd.FindElement(selenium.ByCSSSelector, "#pipelines-new")
			return err == nil
		}, 15*time.Second)
	})
}

type waitForFn func(*testing.T, selenium.WebDriver) bool

func waitFor(t *testing.T, wd selenium.WebDriver, wffn waitForFn, d time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var found bool
	for !found {
		select {
		case <-ctx.Done():
			goto END
		default:
		}
		found = wffn(t, wd)
	}

END:
	if !found {
		screenshot(t, wd)
	}
	require.True(t, found)
}

func eqText(by, value, txt string) waitForFn {
	return func(t *testing.T, wd selenium.WebDriver) bool {
		we, err := wd.FindElement(by, value)
		if err != nil {
			return false
		}

		weTxt, err := we.Text()
		if err != nil {
			//require.NoError(t, err)
			return false
		}

		return weTxt == txt
	}
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// setEditorContent sets the CodeMirror editor content via JavaScript,
// bypassing contenteditable issues with Selenium WebDriver.
func setEditorContent(t *testing.T, wd selenium.WebDriver, content string) {
	t.Helper()
	_, err := wd.ExecuteScript(`
		var view = window._pikoEditor;
		if (view) {
			view.dispatch({changes: {from: 0, to: view.state.doc.length, insert: arguments[0]}});
		}
		// Also sync hidden textarea as fallback
		var ta = document.getElementById('pipeline');
		if (ta) { ta.value = arguments[0]; }
	`, []interface{}{content})
	require.NoError(t, err)
}

func screenshot(t *testing.T, wd selenium.WebDriver) {
	b, err := wd.Screenshot()
	require.NoError(t, err)

	img, _, err := image.Decode(bytes.NewReader(b))
	require.NoError(t, err)

	f, err := os.Create("screenshot.png")
	require.NoError(t, err)
	defer f.Close()

	err = png.Encode(f, img)
	require.NoError(t, err)
}
