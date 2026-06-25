//go:build integration

package selenium_test

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
	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
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

			// After password change, the forced-change flow redirects to "/"
			// Wait for either the toast OR the redirect to Teams page
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				// Check if we've been redirected to teams page
				el, err := wd.FindElement(selenium.ByCSSSelector, "#breadcrumb")
				if err == nil {
					txt, _ := el.Text()
					if txt == "Teams" {
						return true
					}
				}
				// Or check for toast
				el, err = wd.FindElement(selenium.ByCSSSelector, ".piko-toast")
				if err == nil {
					txt, _ := el.Text()
					if strings.Contains(txt, "Password changed") {
						return true
					}
				}
				return false
			}, 20*time.Second)

			// Ensure we're on the teams page
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

			// Wait for user select to load (NewMemberRow has user + role selects)
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				opts, err := wd.FindElements(selenium.ByCSSSelector, "#username option")
				require.NoError(t, err)

				return len(opts) >= 1
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
			// Role dropdowns: admin has one for each member
			roleSelects, err := wd.FindElements(selenium.ByCSSSelector, "select.form-select-sm")
			require.NoError(t, err)
			require.Equal(t, 2, len(roleSelects))

			// First member (admin) should have "admin" selected
			val1, err := roleSelects[0].GetAttribute("value")
			require.NoError(t, err)
			require.Equal(t, "admin", val1)

			// Second member (pepito) should have "maintainer" selected (default from migration)
			val2, err := roleSelects[1].GetAttribute("value")
			require.NoError(t, err)
			require.Equal(t, "maintainer", val2)

			// Change pepito's role to admin via the select
			opts, err := roleSelects[1].FindElements(selenium.ByCSSSelector, "option[value='admin']")
			require.NoError(t, err)
			require.Equal(t, 1, len(opts))
			err = opts[0].Click()
			require.NoError(t, err)

			time.Sleep(500 * time.Millisecond)

			// Verify the role was updated
			roleSelects, err = wd.FindElements(selenium.ByCSSSelector, "select.form-select-sm")
			require.NoError(t, err)
			val2, err = roleSelects[1].GetAttribute("value")
			require.NoError(t, err)
			require.Equal(t, "admin", val2)
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
		t.Run("Editor Vars Tab", func(t *testing.T) {
			// Navigate to pipeline editor
			epBtn, err := wd.FindElement(selenium.ByCSSSelector, "#edit-pipeline")
			require.NoError(t, err)
			err = epBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Update Pipeline"), 5*time.Second)

			// Click vars.json tab
			varsTab, err := wd.FindElement(selenium.ByCSSSelector, "#tab-vars")
			require.NoError(t, err)
			err = varsTab.Click()
			require.NoError(t, err)

			// Verify vars textarea is visible
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#vars")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			// Click back to HCL tab
			hclTab, err := wd.FindElement(selenium.ByCSSSelector, "#tab-hcl")
			require.NoError(t, err)
			err = hclTab.Click()
			require.NoError(t, err)

			// Verify code area is visible again
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#code-area")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)
		})
		t.Run("Editor Blocks Panel", func(t *testing.T) {
			// Blocks panel should show blocks from the HCL
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				items, err := wd.FindElements(selenium.ByCSSSelector, ".piko-blocks-item")
				return err == nil && len(items) >= 2 // resource + job
			}, 5*time.Second)
		})
		t.Run("Editor Edit Prefills Data", func(t *testing.T) {
			// Verify editor has the existing HCL content
			val, err := wd.ExecuteScript("return window._pikoEditor ? window._pikoEditor.state.doc.toString() : ''", nil)
			require.NoError(t, err)
			content, ok := val.(string)
			require.True(t, ok)
			require.Contains(t, content, "my_cron_edit", "editor should contain existing pipeline HCL")

			// Verify name field has existing pipeline name
			nameInput, err := wd.FindElement(selenium.ByCSSSelector, "#name")
			require.NoError(t, err)
			nameVal, err := nameInput.GetAttribute("value")
			require.NoError(t, err)
			require.Equal(t, "cron", nameVal)

			// Navigate back to pipeline show
			err = wd.Get(pikoURL + "/teams/main/pipelines/cron")
			require.NoError(t, err)
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)
		})
		t.Run("Editor Fullscreen Mode", func(t *testing.T) {
			// Navigate to editor
			epBtn, err := wd.FindElement(selenium.ByCSSSelector, "#edit-pipeline")
			require.NoError(t, err)
			err = epBtn.Click()
			require.NoError(t, err)
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Update Pipeline"), 5*time.Second)

			// Click fullscreen button
			_, err = wd.ExecuteScript("document.getElementById('fullscreen-btn').click()", nil)
			require.NoError(t, err)

			// Verify body has piko-fullscreen class
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				val, err := wd.ExecuteScript("return document.body.classList.contains('piko-fullscreen')", nil)
				if err != nil {
					return false
				}
				b, _ := val.(bool)
				return b
			}, 5*time.Second)

			// Send Escape to exit fullscreen
			_, err = wd.ExecuteScript("document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))", nil)
			require.NoError(t, err)

			// Verify fullscreen class is removed
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				val, err := wd.ExecuteScript("return !document.body.classList.contains('piko-fullscreen')", nil)
				if err != nil {
					return false
				}
				b, _ := val.(bool)
				return b
			}, 5*time.Second)
		})
		t.Run("Editor Graph Node Click Jumps To Block", func(t *testing.T) {
			// We're in the editor. Wait for graph preview to render.
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "#graph svg")
				return err == nil
			}, 5*time.Second)

			// Get current editor cursor position
			posBefore, err := wd.ExecuteScript("return window._pikoEditor ? window._pikoEditor.state.selection.main.head : -1", nil)
			require.NoError(t, err)

			// Click a graph node via JS (job node)
			_, err = wd.ExecuteScript(`
				var nodes = document.querySelectorAll('#graph g.node');
				if (nodes.length > 0) {
					nodes[nodes.length - 1].dispatchEvent(new MouseEvent('click', {bubbles: true}));
				}
			`, nil)
			require.NoError(t, err)

			// Verify editor cursor position changed (block was selected)
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				posAfter, err := wd.ExecuteScript("return window._pikoEditor ? window._pikoEditor.state.selection.main.head : -1", nil)
				if err != nil {
					return false
				}
				// Position should be different from before (editor jumped to block)
				return posAfter != posBefore
			}, 5*time.Second)
		})
		t.Run("Editor HCL Error Diagnostics", func(t *testing.T) {
			// Enter invalid HCL to trigger error diagnostics
			setEditorContent(t, wd, `
resource "cron" "my_cron_edit" {
  check_interval = "@every 1m"
}

job "gen" {
  get "cron" "my_cron_edit" {
    trigger = true
  }
  task "echo" {
    run "INVALID_RUNNER_TYPE" {
`)

			// Wait for the debounced preview to fire and error to appear.
			// The error badge on blocks button should update.
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				// Check for CodeMirror lint markers or error badge
				val, err := wd.ExecuteScript(`
					var badge = document.querySelector('.piko-error-badge');
					if (badge && badge.textContent && badge.textContent.trim() !== '') return true;
					var markers = document.querySelectorAll('.cm-lint-marker-error, .cm-lintRange-error');
					return markers.length > 0;
				`, nil)
				if err != nil {
					return false
				}
				b, _ := val.(bool)
				return b
			}, 10*time.Second)

			// Restore valid HCL so subsequent tests work
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

			// Submit to save valid state
			upBtn, err := wd.FindElement(selenium.ByCSSSelector, "form button[type='submit']")
			require.NoError(t, err)
			err = upBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)
		})
		t.Run("View Switcher", func(t *testing.T) {
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")
				return err == nil
			}, 5*time.Second)

			graphBtn, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-btn[data-view='graph']")
			require.NoError(t, err)
			require.True(t, hasClass(graphBtn, "active"), "graph button should be active by default")

			graphView, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-graph")
			require.NoError(t, err)
			gDisp, err := graphView.IsDisplayed()
			require.NoError(t, err)
			require.True(t, gDisp, "graph view should be visible")

			listBtn, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-btn[data-view='list']")
			require.NoError(t, err)
			err = listBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-list")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			jobRows, err := wd.FindElements(selenium.ByCSSSelector, ".piko-job-row")
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(jobRows), 1, "should have at least one job row")

			_, err = wd.FindElement(selenium.ByCSSSelector, ".piko-rsel-container")
			require.NoError(t, err, "resource selector should exist in list view")

			graphBtn, err = wd.FindElement(selenium.ByCSSSelector, ".piko-view-btn[data-view='graph']")
			require.NoError(t, err)
			err = graphBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-graph")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)
		})
		t.Run("Gear Panel", func(t *testing.T) {
			gearPanel, err := wd.FindElement(selenium.ByCSSSelector, "#gear-panel")
			require.NoError(t, err)
			require.False(t, hasClass(gearPanel, "open"), "gear panel should be closed initially")

			toggleBtn, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-gear-panel")
			require.NoError(t, err)
			err = toggleBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#gear-panel", "open", true), 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#gear-hide-intermediates")
			require.NoError(t, err, "hide intermediates checkbox should exist")

			_, err = wd.FindElement(selenium.ByCSSSelector, "#gear-group-parallel")
			require.NoError(t, err, "group parallel checkbox should exist")

			hiCheckbox, err := wd.FindElement(selenium.ByCSSSelector, "#gear-hide-intermediates")
			require.NoError(t, err)
			err = hiCheckbox.Click()
			require.NoError(t, err)

			time.Sleep(500 * time.Millisecond)

			err = hiCheckbox.Click()
			require.NoError(t, err)

			err = toggleBtn.Click()
			require.NoError(t, err)
			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#gear-panel", "open", false), 5*time.Second)
		})
		t.Run("Share Panel", func(t *testing.T) {
			shareToggle, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-share-panel")
			require.NoError(t, err)
			err = shareToggle.Click()
			require.NoError(t, err)

			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#share-panel", "open", true), 5*time.Second)

			// Wait for SVG URL to be populated (JS sets .value, not HTML attribute)
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				val, err := wd.ExecuteScript("return document.getElementById('share-svg-url').value", nil)
				if err != nil {
					return false
				}
				s, _ := val.(string)
				return s != ""
			}, 5*time.Second)

			svgVal, err := wd.ExecuteScript("return document.getElementById('share-svg-url').value", nil)
			require.NoError(t, err)
			require.Contains(t, svgVal.(string), ".svg", "SVG URL should contain .svg")

			pngVal, err := wd.ExecuteScript("return document.getElementById('share-png-url').value", nil)
			require.NoError(t, err)
			require.Contains(t, pngVal.(string), ".png", "PNG URL should contain .png")

			mdVal, err := wd.ExecuteScript("return document.getElementById('share-md-url').value", nil)
			require.NoError(t, err)
			require.Contains(t, mdVal.(string), "![", "Markdown URL should contain ![ prefix")

			copyBtns, err := wd.FindElements(selenium.ByCSSSelector, ".piko-share-copy")
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(copyBtns), 1, "should have copy buttons")

			// Close share panel and verify it's fully closed before next test
			err = shareToggle.Click()
			require.NoError(t, err)
			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#share-panel", "open", false), 5*time.Second)
		})
		t.Run("Resources Panel", func(t *testing.T) {
			resToggle, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-resources-panel")
			require.NoError(t, err)
			err = resToggle.Click()
			require.NoError(t, err)

			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", true), 5*time.Second)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, ".piko-resource-card[data-canonical='cron.my_cron_edit']")
				return err == nil
			}, 5*time.Second)

			cardName, err := wd.FindElement(selenium.ByCSSSelector, ".piko-resource-card[data-canonical='cron.my_cron_edit'] .piko-resource-card-name")
			require.NoError(t, err)
			nameTxt, err := cardName.Text()
			require.NoError(t, err)
			require.Contains(t, nameTxt, "cron.my_cron_edit")

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-resource-card[data-canonical='cron.my_cron_edit'] .piko-resource-card-type")
				if err != nil {
					return false
				}
				txt, err := el.Text()
				if err != nil {
					return false
				}
				return strings.Contains(txt, "cron")
			}, 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, ".check-resource-now")
			require.NoError(t, err, "admin should see check-resource-now button")

			closeBtn, err := wd.FindElement(selenium.ByCSSSelector, "#close-resources-panel")
			require.NoError(t, err)
			err = closeBtn.Click()
			require.NoError(t, err)
			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", false), 5*time.Second)
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
		t.Run("Resource Version Row Expand", func(t *testing.T) {
			// We're on the resource versions page with at least 1 version
			// Click version row header to expand details
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				headers, err := wd.FindElements(selenium.ByCSSSelector, ".piko-version-row-header")
				return err == nil && len(headers) > 0
			}, 5*time.Second)

			headers, err := wd.FindElements(selenium.ByCSSSelector, ".piko-version-row-header")
			require.NoError(t, err)
			err = headers[0].Click()
			require.NoError(t, err)

			// Verify body becomes visible
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				bodies, err := wd.FindElements(selenium.ByCSSSelector, ".piko-version-row-body")
				if err != nil || len(bodies) == 0 {
					return false
				}
				d, err := bodies[0].IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			// Verify version table exists inside
			_, err = wd.FindElement(selenium.ByCSSSelector, ".piko-version-table")
			require.NoError(t, err)
		})
		t.Run("Resource Pin Unpin", func(t *testing.T) {
			// Find pin button on the version row
			pinBtn, err := wd.FindElement(selenium.ByCSSSelector, ".pin-version")
			require.NoError(t, err)

			err = pinBtn.Click()
			require.NoError(t, err)

			// Wait for pinned banner to appear
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#pinned-version-banner")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			// Click unpin button on the banner
			unpinBtn, err := wd.FindElement(selenium.ByCSSSelector, "#unpin-banner")
			require.NoError(t, err)
			err = unpinBtn.Click()
			require.NoError(t, err)

			// Verify banner disappears
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#pinned-version-banner")
				if err != nil {
					return true // element gone
				}
				d, err := el.IsDisplayed()
				return err == nil && !d
			}, 5*time.Second)
		})
		t.Run("Webhook Panel", func(t *testing.T) {
			// Admin should see webhook toggle in the split dropdown next to trigger button.
			// First click the dropdown toggle button to open the menu.
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, ".btn-group .dropdown-toggle")
				return err == nil
			}, 5*time.Second)

			ddToggle, err := wd.FindElement(selenium.ByCSSSelector, ".btn-group .dropdown-toggle")
			require.NoError(t, err)
			err = ddToggle.Click()
			require.NoError(t, err)

			// Now click the webhook panel link inside the dropdown
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-webhook-panel")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			_, err = wd.ExecuteScript("document.getElementById('toggle-webhook-panel').click()", nil)
			require.NoError(t, err)

			// Verify webhook panel is visible
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#webhook-panel")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			// Verify webhook URL is populated
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#webhook-url")
				if err != nil {
					return false
				}
				txt, err := el.Text()
				return err == nil && txt != ""
			}, 5*time.Second)

			// Verify copy and regenerate buttons exist
			_, err = wd.FindElement(selenium.ByCSSSelector, "#copy-webhook")
			require.NoError(t, err)
			_, err = wd.FindElement(selenium.ByCSSSelector, "#regenerate-webhook")
			require.NoError(t, err)
		})
		t.Run("Version Scope Banner", func(t *testing.T) {
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron")
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")
				return err == nil
			}, 5*time.Second)

			resToggle, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-resources-panel")
			require.NoError(t, err)
			err = resToggle.Click()
			require.NoError(t, err)

			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", true), 5*time.Second)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, ".piko-resource-card[data-canonical='cron.my_cron_edit']")
				return err == nil
			}, 5*time.Second)

			expandToggle, err := wd.FindElement(selenium.ByCSSSelector, ".piko-resource-expand-toggle[data-canonical='cron.my_cron_edit']")
			require.NoError(t, err)
			err = expandToggle.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, ".piko-panel-track-btn")
				return err == nil
			}, 5*time.Second)

			trackBtn, err := wd.FindElement(selenium.ByCSSSelector, ".piko-panel-track-btn")
			require.NoError(t, err)
			err = trackBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#version-scope-banner")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			bannerRes, err := wd.FindElement(selenium.ByCSSSelector, "#version-banner-resource")
			require.NoError(t, err)
			resTxt, err := bannerRes.Text()
			require.NoError(t, err)
			require.Equal(t, "cron.my_cron_edit", resTxt)

			bannerProgress, err := wd.FindElement(selenium.ByCSSSelector, "#version-banner-progress")
			require.NoError(t, err)
			progTxt, err := bannerProgress.Text()
			require.NoError(t, err)
			require.Contains(t, progTxt, "completed")

			clearBtn, err := wd.FindElement(selenium.ByCSSSelector, "#clear-version-scope")
			require.NoError(t, err)
			err = clearBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#version-scope-banner")
				if err != nil {
					return true
				}
				d, err := el.IsDisplayed()
				return err == nil && !d
			}, 5*time.Second)

			closeBtn, err := wd.FindElement(selenium.ByCSSSelector, "#close-resources-panel")
			require.NoError(t, err)
			err = closeBtn.Click()
			require.NoError(t, err)
			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", false), 5*time.Second)
		})
		t.Run("Job Builds", func(t *testing.T) {
			// Navigate to pipeline page (may already be here after Version Scope Banner)
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron")
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

			// The first resource check triggers a build, so wait for it.
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				builds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
				require.NoError(t, err)

				return len(builds) >= 1
			}, 5*time.Second)

			// Count current builds, then trigger one more manually.
			builds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
			require.NoError(t, err)
			countBefore := len(builds)

			tjBtn, err := wd.FindElement(selenium.ByCSSSelector, "#trigger-job")
			require.NoError(t, err)

			err = tjBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				builds, err := wd.FindElements(selenium.ByCSSSelector, "#builds-tabs>.piko-build-tab")
				require.NoError(t, err)

				return len(builds) >= countBefore+1
			}, 5*time.Second)

		})
		t.Run("Build Logs Steps", func(t *testing.T) {
			// We're on the job builds page from the previous test.
			// Wait for a completed build so steps are available.
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				els, err := wd.FindElements(selenium.ByCSSSelector, ".piko-step-row")
				return err == nil && len(els) >= 1
			}, 10*time.Second)

			// Find step headers and verify they exist
			headers, err := wd.FindElements(selenium.ByCSSSelector, ".piko-step-row-header")
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(headers), 1, "should have at least one step header")

			// Click the first step header to expand it (use JS to avoid scroll issues)
			_, err = wd.ExecuteScript("document.querySelector('.piko-step-row-header').click()", nil)
			require.NoError(t, err)

			// Verify step body becomes visible
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				val, err := wd.ExecuteScript("var b = document.querySelector('.piko-step-row-body'); return b ? b.style.display : 'none'", nil)
				if err != nil {
					return false
				}
				s, _ := val.(string)
				return s == "block" || s == ""
			}, 5*time.Second)

			// Click again to collapse
			_, err = wd.ExecuteScript("document.querySelector('.piko-step-row-header').click()", nil)
			require.NoError(t, err)

			time.Sleep(300 * time.Millisecond)

			val, err := wd.ExecuteScript("var b = document.querySelector('.piko-step-row-body'); return b ? b.style.display : 'none'", nil)
			require.NoError(t, err)
			s, _ := val.(string)
			require.Equal(t, "none", s, "step body should be collapsed after second click")
		})
		t.Run("Build Logs Retry Button", func(t *testing.T) {
			// Wait for a completed build — retry button should appear
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, ".piko-retry-build")
				return err == nil
			}, 10*time.Second)
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
		t.Run("Pipeline Pause Unpause", func(t *testing.T) {
			// Navigate to pipeline show page
			ppBtn, err := wd.FindElement(selenium.ByCSSSelector, ".card")
			require.NoError(t, err)
			err = ppBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)

			// Find and click pause button
			pauseBtn, err := wd.FindElement(selenium.ByCSSSelector, "#pause-pipeline")
			require.NoError(t, err)
			err = pauseBtn.Click()
			require.NoError(t, err)

			// Verify unpause button appears
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "#unpause-pipeline")
				return err == nil
			}, 5*time.Second)

			// Click unpause
			unpauseBtn, err := wd.FindElement(selenium.ByCSSSelector, "#unpause-pipeline")
			require.NoError(t, err)
			err = unpauseBtn.Click()
			require.NoError(t, err)

			// Verify pause button returns
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "#pause-pipeline")
				return err == nil
			}, 5*time.Second)

			// Navigate back to pipelines list for next test
			err = wd.Get(pikoURL + "/teams/main/pipelines")
			require.NoError(t, err)
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)
		})
		t.Run("Users Page", func(t *testing.T) {
			// Open navbar dropdown and click Users
			navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
			require.NoError(t, err)
			err = navLink.Click()
			require.NoError(t, err)

			usersLink, err := wd.FindElement(selenium.ByCSSSelector, "#nav-users")
			require.NoError(t, err)
			err = usersLink.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Users"), 5*time.Second)

			// Verify users table renders with rows
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				rows, err := wd.FindElements(selenium.ByCSSSelector, "#users-table-body>tr")
				return err == nil && len(rows) >= 2 // at least admin + pepito
			}, 5*time.Second)

			// Count initial users
			initialRows, err := wd.FindElements(selenium.ByCSSSelector, "#users-table-body>tr")
			require.NoError(t, err)
			initialCount := len(initialRows)

			t.Run("Create User", func(t *testing.T) {
				// Use JS click because badge may obscure the button
				_, err := wd.ExecuteScript("document.getElementById('user-new').click()", nil)
				require.NoError(t, err)

				waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "New User"), 5*time.Second)

				username, err := wd.FindElement(selenium.ByCSSSelector, "#username")
				require.NoError(t, err)
				username.SendKeys("testuser")

				fullName, err := wd.FindElement(selenium.ByCSSSelector, "#full_name")
				require.NoError(t, err)
				fullName.SendKeys("Test User")

				password, err := wd.FindElement(selenium.ByCSSSelector, "#password")
				require.NoError(t, err)
				password.SendKeys("testpass123")

				submitBtn, err := wd.FindElement(selenium.ByCSSSelector, "#user-create-form button[type='submit']")
				require.NoError(t, err)
				err = submitBtn.Click()
				require.NoError(t, err)

				// Should redirect to user show page
				waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Edit User: testuser"), 5*time.Second)
			})

			t.Run("Edit User", func(t *testing.T) {
				// We're on the user show page from the create test
				fullNameInput, err := wd.FindElement(selenium.ByCSSSelector, "#full_name")
				require.NoError(t, err)
				err = fullNameInput.Clear()
				require.NoError(t, err)
				fullNameInput.SendKeys("Updated Test User")

				submitBtn, err := wd.FindElement(selenium.ByCSSSelector, "#user-form button[type='submit']")
				require.NoError(t, err)
				err = submitBtn.Click()
				require.NoError(t, err)

				// Wait for success feedback (toast or page reload)
				time.Sleep(1 * time.Second)

				// Verify the name was updated by checking the field value
				waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
					el, err := wd.FindElement(selenium.ByCSSSelector, "#full_name")
					if err != nil {
						return false
					}
					val, err := el.GetAttribute("value")
					return err == nil && val == "Updated Test User"
				}, 5*time.Second)
			})

			t.Run("Reset Password", func(t *testing.T) {
				// We're on the user show page
				newPw, err := wd.FindElement(selenium.ByCSSSelector, "#new_password")
				require.NoError(t, err)
				newPw.SendKeys("newpass456")

				resetBtn, err := wd.FindElement(selenium.ByCSSSelector, "#reset-password-form button[type='submit']")
				require.NoError(t, err)
				err = resetBtn.Click()
				require.NoError(t, err)

				// Wait for success feedback
				time.Sleep(1 * time.Second)
			})

			t.Run("Delete User", func(t *testing.T) {
				// We're on the user show page
				deleteBtn, err := wd.FindElement(selenium.ByCSSSelector, "#delete-user")
				require.NoError(t, err)
				err = deleteBtn.Click()
				require.NoError(t, err)

				// Accept confirmation dialog
				err = wd.AcceptAlert()
				require.NoError(t, err)

				// Should redirect to users list
				waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Users"), 5*time.Second)

				// Verify user count is back to initial
				waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
					rows, err := wd.FindElements(selenium.ByCSSSelector, "#users-table-body>tr")
					return err == nil && len(rows) == initialCount
				}, 5*time.Second)
			})

			// Navigate back to pipelines for next test
			err = wd.Get(pikoURL + "/teams/main/pipelines")
			require.NoError(t, err)
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)
		})
		t.Run("Workers Page", func(t *testing.T) {
			// Open navbar dropdown and click Workers
			navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
			require.NoError(t, err)
			err = navLink.Click()
			require.NoError(t, err)

			workersLink, err := wd.FindElement(selenium.ByCSSSelector, "#nav-workers")
			require.NoError(t, err)
			err = workersLink.Click()
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "h1", "Workers"), 5*time.Second)

			// Verify workers table exists (workers may not be registered via HTTP API in test mode)
			_, err = wd.FindElement(selenium.ByCSSSelector, "#workers-table-body")
			require.NoError(t, err, "workers table body should exist")

			// Navigate back to pipelines for next test
			err = wd.Get(pikoURL + "/teams/main/pipelines")
			require.NoError(t, err)
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)
		})
		t.Run("Dark Mode Toggle", func(t *testing.T) {
			// Open navbar dropdown and click theme toggle via JS (toggle may not be scrollable)
			navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
			require.NoError(t, err)
			err = navLink.Click()
			require.NoError(t, err)

			time.Sleep(300 * time.Millisecond)

			// Use JS click because the toggle is inside a dropdown and may not be scrollable
			_, err = wd.ExecuteScript("document.getElementById('theme-toggle').closest('a').click()", nil)
			require.NoError(t, err)

			// Verify data-theme="dark" is set on html element
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				val, err := wd.ExecuteScript("return document.documentElement.getAttribute('data-theme')", nil)
				if err != nil {
					return false
				}
				s, _ := val.(string)
				return s == "dark"
			}, 5*time.Second)

			// Close dropdown
			logo, err := wd.FindElement(selenium.ByCSSSelector, "#logo")
			require.NoError(t, err)
			logo.Click()

			// Reload page and verify dark mode persisted
			err = wd.Get(pikoURL + "/teams/main/pipelines")
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				val, err := wd.ExecuteScript("return document.documentElement.getAttribute('data-theme')", nil)
				if err != nil {
					return false
				}
				s, _ := val.(string)
				return s == "dark"
			}, 5*time.Second)

			// Revert to light mode for remaining tests
			navLink, err = wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
			require.NoError(t, err)
			err = navLink.Click()
			require.NoError(t, err)

			time.Sleep(300 * time.Millisecond)

			_, err = wd.ExecuteScript("document.getElementById('theme-toggle').closest('a').click()", nil)
			require.NoError(t, err)

			// Close dropdown
			logo, err = wd.FindElement(selenium.ByCSSSelector, "#logo")
			require.NoError(t, err)
			logo.Click()
		})
		t.Run("Not Found Redirect", func(t *testing.T) {
			err := wd.Get(pikoURL + "/this-route-does-not-exist")
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams"), 5*time.Second)
		})
		t.Run("Direct Deep URL Navigation", func(t *testing.T) {
			// Navigate directly to pipeline show without clicking through
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron")
			require.NoError(t, err)

			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)

			// Verify graph renders
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")
				return err == nil
			}, 5*time.Second)

			// Navigate back to pipelines list for delete test
			err = wd.Get(pikoURL + "/teams/main/pipelines")
			require.NoError(t, err)
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines"), 5*time.Second)
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

				// Wait for user select to load (NewMemberRow has user + role selects)
				waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
					opts, err := wd.FindElements(selenium.ByCSSSelector, "#username option")
					require.NoError(t, err)

					return len(opts) >= 1
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
		t.Run("Export Database Visible", func(t *testing.T) {
			navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
			require.NoError(t, err)

			err = navLink.Click()
			require.NoError(t, err)

			// Admin should see the export database link
			exportLink, err := wd.FindElement(selenium.ByCSSSelector, "#nav-export-db")
			require.NoError(t, err, "admin should see export database link")

			txt, err := exportLink.Text()
			require.NoError(t, err)
			require.Contains(t, txt, "Export Database")

			// Also verify admin-only Users link is visible
			_, err = wd.FindElement(selenium.ByCSSSelector, "#nav-users")
			require.NoError(t, err, "admin should see users link")

			// Close the dropdown by clicking elsewhere
			logo, err := wd.FindElement(selenium.ByCSSSelector, "#logo")
			require.NoError(t, err)
			logo.Click()
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
		t.Run("Export Database Not Visible", func(t *testing.T) {
			navLink, err := wd.FindElement(selenium.ByCSSSelector, ".navbar .nav-link")
			require.NoError(t, err)

			err = navLink.Click()
			require.NoError(t, err)

			// Non-admin should NOT see the export database link
			_, err = wd.FindElement(selenium.ByCSSSelector, "#nav-export-db")
			require.Error(t, err, "non-admin should not see export database link")

			// Non-admin should NOT see the users link either
			_, err = wd.FindElement(selenium.ByCSSSelector, "#nav-users")
			require.Error(t, err, "non-admin should not see users link")

			// Close the dropdown by clicking elsewhere
			logo, err := wd.FindElement(selenium.ByCSSSelector, "#logo")
			require.NoError(t, err)
			logo.Click()
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

			// Non-admin members see roles as plain text, not dropdowns
			roleSelects, err := wd.FindElements(selenium.ByCSSSelector, "select.form-select-sm")
			require.NoError(t, err)
			require.Equal(t, 0, len(roleSelects), "non-admin should not see role dropdowns")
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

			// Members CAN see pause/unpause (controlled by isMember, not isAdmin)
			// Just verify they don't see edit/delete (already checked above)
		})
		t.Run("Navigate to Edit Pipeline redirects", func(t *testing.T) {
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron/edit")
			require.NoError(t, err)

			// Should be redirected back to pipeline show page
			waitFor(t, wd, eqText(selenium.ByCSSSelector, "#breadcrumb", "Teams\nMain\nPipelines\ncron"), 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, "#edit-pipeline")
			require.Error(t, err)
		})
		t.Run("View Switcher", func(t *testing.T) {
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")
				return err == nil
			}, 5*time.Second)

			listBtn, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-btn[data-view='list']")
			require.NoError(t, err)
			err = listBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-list")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			graphBtn, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-btn[data-view='graph']")
			require.NoError(t, err)
			err = graphBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-graph")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)
		})
		t.Run("Resources Panel", func(t *testing.T) {
			resToggle, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-resources-panel")
			require.NoError(t, err)
			err = resToggle.Click()
			require.NoError(t, err)

			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", true), 5*time.Second)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				cards, err := wd.FindElements(selenium.ByCSSSelector, ".piko-resource-card")
				return err == nil && len(cards) > 0
			}, 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, ".check-resource-now")
			require.NoError(t, err, "member should see check-resource-now button")

			closeBtn, err := wd.FindElement(selenium.ByCSSSelector, "#close-resources-panel")
			require.NoError(t, err)
			err = closeBtn.Click()
			require.NoError(t, err)
			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", false), 5*time.Second)
		})
		t.Run("Share Panel", func(t *testing.T) {
			shareToggle, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-share-panel")
			require.NoError(t, err)
			err = shareToggle.Click()
			require.NoError(t, err)

			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#share-panel", "open", true), 5*time.Second)

			// Wait for SVG URL to be populated (JS sets .value, not HTML attribute)
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				val, err := wd.ExecuteScript("return document.getElementById('share-svg-url').value", nil)
				if err != nil {
					return false
				}
				s, _ := val.(string)
				return s != ""
			}, 5*time.Second)

			svgVal, err := wd.ExecuteScript("return document.getElementById('share-svg-url').value", nil)
			require.NoError(t, err)
			require.Contains(t, svgVal.(string), ".svg")

			pngVal, err := wd.ExecuteScript("return document.getElementById('share-png-url').value", nil)
			require.NoError(t, err)
			require.Contains(t, pngVal.(string), ".png")

			err = shareToggle.Click()
			require.NoError(t, err)
			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#share-panel", "open", false), 5*time.Second)
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
		loginReq, err := http.NewRequest(http.MethodPost, pikoURL+"/login", bytes.NewReader(loginBody))
		require.NoError(t, err)
		loginReq.Header.Set("Content-Type", "application/json")
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
		updateReq, err := http.NewRequest(http.MethodPut, pikoURL+"/teams/main/pipelines/cron", bytes.NewReader(updateBody))
		require.NoError(t, err)
		updateReq.Header.Set("Content-Type", "application/json")
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

		t.Run("ViewSwitcherPublic", func(t *testing.T) {
			listBtn, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-btn[data-view='list']")
			require.NoError(t, err)
			err = listBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-list")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)

			graphBtn, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-btn[data-view='graph']")
			require.NoError(t, err)
			err = graphBtn.Click()
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, ".piko-view-graph")
				if err != nil {
					return false
				}
				d, err := el.IsDisplayed()
				return err == nil && d
			}, 5*time.Second)
		})

		t.Run("ResourcesPanelPublicHidden", func(t *testing.T) {
			resToggle, err := wd.FindElement(selenium.ByCSSSelector, "#toggle-resources-panel")
			require.NoError(t, err)
			err = resToggle.Click()
			require.NoError(t, err)

			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", true), 5*time.Second)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				cards, err := wd.FindElements(selenium.ByCSSSelector, ".piko-resource-card")
				return err == nil && len(cards) > 0
			}, 5*time.Second)

			_, err = wd.FindElement(selenium.ByCSSSelector, ".check-resource-now")
			require.Error(t, err, "public viewer should not see check-resource-now button")

			closeBtn, err := wd.FindElement(selenium.ByCSSSelector, "#close-resources-panel")
			require.NoError(t, err)
			err = closeBtn.Click()
			require.NoError(t, err)
			waitFor(t, wd, waitForClass(selenium.ByCSSSelector, "#pipeline-resources-panel", "open", false), 5*time.Second)
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

		t.Run("NoPipelineActions", func(t *testing.T) {
			// Navigate to public pipeline show page
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron")
			require.NoError(t, err)

			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				_, err := wd.FindElement(selenium.ByCSSSelector, "div#pipeline-graph>svg")
				return err == nil
			}, 5*time.Second)

			// Public viewer should NOT see any pipeline action buttons
			_, err = wd.FindElement(selenium.ByCSSSelector, "#pause-pipeline")
			require.Error(t, err, "public viewer should not see pause button")
			_, err = wd.FindElement(selenium.ByCSSSelector, "#unpause-pipeline")
			require.Error(t, err, "public viewer should not see unpause button")
			_, err = wd.FindElement(selenium.ByCSSSelector, "#edit-pipeline")
			require.Error(t, err, "public viewer should not see edit button")
			_, err = wd.FindElement(selenium.ByCSSSelector, "#delete-pipeline")
			require.Error(t, err, "public viewer should not see delete button")
		})

		t.Run("PublicResourceVersions", func(t *testing.T) {
			// Navigate to resource versions page as public viewer (not logged in)
			// First get the resource link from the graph
			err := wd.Get(pikoURL + "/teams/main/pipelines/cron")
			require.NoError(t, err)

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

			// Should be able to view the resource versions page
			waitFor(t, wd, func(t *testing.T, wd selenium.WebDriver) bool {
				el, err := wd.FindElement(selenium.ByCSSSelector, "#resource-versions")
				return err == nil && el != nil
			}, 5*time.Second)

			// Public viewer should NOT see trigger resource button
			_, err = wd.FindElement(selenium.ByCSSSelector, "#trigger-resource")
			require.Error(t, err, "public viewer should not see trigger resource button")

			// Public viewer should NOT see webhook panel toggle
			_, err = wd.FindElement(selenium.ByCSSSelector, "#toggle-webhook-panel")
			require.Error(t, err, "public viewer should not see webhook panel toggle")

			// If versions exist, public viewer should NOT see track/trigger/pin buttons
			versions, _ := wd.FindElements(selenium.ByCSSSelector, "#resource-versions>div")
			if len(versions) > 0 {
				_, err = wd.FindElement(selenium.ByCSSSelector, ".track-version")
				require.Error(t, err, "public viewer should not see track version button")
				_, err = wd.FindElement(selenium.ByCSSSelector, ".trigger-version")
				require.Error(t, err, "public viewer should not see trigger version button")
				_, err = wd.FindElement(selenium.ByCSSSelector, ".pin-version")
				require.Error(t, err, "public viewer should not see pin version button")
			}
		})

		t.Run("ExportEndpoint_AdminAllowed", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, pikoURL+"/admin/export", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+adminJWT)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode, "admin should be able to export")
			require.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
			require.Contains(t, resp.Header.Get("Content-Disposition"), "pikoci.db")
		})

		t.Run("ExportEndpoint_UnauthenticatedRejected", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, pikoURL+"/admin/export", nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusBadRequest, resp.StatusCode, "unauthenticated should be rejected")
		})

		t.Run("ExportEndpoint_NonAdminRejected", func(t *testing.T) {
			// Login as pepito (non-admin) via HTTP
			pepitoBody, _ := json.Marshal(thttp.UserLoginRequest{
				Username: "pepito",
				Password: "pepito",
			})
			pepitoReq, err := http.NewRequest(http.MethodPost, pikoURL+"/login", bytes.NewReader(pepitoBody))
			require.NoError(t, err)
			pepitoReq.Header.Set("Content-Type", "application/json")
			pepitoResp, err := http.DefaultClient.Do(pepitoReq)
			require.NoError(t, err)
			defer pepitoResp.Body.Close()
			require.Equal(t, http.StatusOK, pepitoResp.StatusCode)
			var plr thttp.UserLoginResponse
			json.NewDecoder(pepitoResp.Body).Decode(&plr)
			require.Empty(t, plr.Err)
			pepitoJWT := plr.Data.JWT

			req, err := http.NewRequest(http.MethodGet, pikoURL+"/admin/export", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+pepitoJWT)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusBadRequest, resp.StatusCode, "non-admin should be rejected")
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
		loginReq, err := http.NewRequest(http.MethodPost, pikoURL+"/login", bytes.NewReader(loginBody))
		require.NoError(t, err)
		loginReq.Header.Set("Content-Type", "application/json")
		loginResp, err := http.DefaultClient.Do(loginReq)
		require.NoError(t, err)
		defer loginResp.Body.Close()
		require.Equal(t, http.StatusOK, loginResp.StatusCode)
		var lr thttp.UserLoginResponse
		json.NewDecoder(loginResp.Body).Decode(&lr)
		require.Empty(t, lr.Err)
		adminJWT := lr.Data.JWT

		// Step 2: Promote pepito to admin on "main" team via HTTP
		updateBody, _ := json.Marshal(thttp.UpdateTeamMemberRequest{Role: "admin"})
		updateReq, err := http.NewRequest(http.MethodPut, pikoURL+"/teams/main/members/pepito", bytes.NewReader(updateBody))
		require.NoError(t, err)
		updateReq.Header.Set("Content-Type", "application/json")
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
		checkReq, err := http.NewRequest(http.MethodGet, pikoURL+"/teams", nil)
		require.NoError(t, err)
		checkReq.Header.Set("Content-Type", "application/json")
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
