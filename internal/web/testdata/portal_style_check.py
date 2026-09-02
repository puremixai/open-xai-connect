from pathlib import Path
import base64
import os
import subprocess


git_ref = os.environ.get("PORTAL_STYLE_GIT_REF")
if git_ref:
    css = subprocess.check_output(
        ["git", "show", f"{git_ref}:internal/web/static/portal.css"],
        cwd=Path.cwd(),
        encoding="utf-8",
    )
else:
    css = (Path.cwd() / "internal/web/static/portal.css").read_text(encoding="utf-8")
html = f"""<!doctype html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><style>{css}</style></head>
<body class="portal-body">
  <header class="portal-topbar">
    <div class="topbar-inner">
      <a class="portal-brand" href="#brand">
        <span class="brand-mark">X</span><span class="brand-name">XAI Connect</span>
      </a>
      <nav class="portal-nav" aria-label="Main">
        <a class="is-active" href="#apps">Apps</a>
        <a href="#create">Create app</a>
        <a href="#review">Reviews</a>
        <a href="#admin">Operations</a>
      </nav>
      <div class="topbar-actions">
        <a class="topbar-link" href="#docs">Documentation</a>
        <span class="user-chip">
          <span class="user-avatar">B</span><span class="user-name">Ben</span><span class="role-label">Administrator</span>
        </span>
        <button class="logout-button" type="button">Sign out</button>
      </div>
    </div>
  </header>
  <main class="portal-main">
    <div class="portal-container">
      <div class="panel-head">
        <h2>Applications</h2>
        <div class="toolbar-actions"><a class="text-link" href="#new">New</a></div>
      </div>
      <a class="button" href="#primary">Create app</a>
      <a class="row-action" href="#detail">Details<span>&gt;</span></a>
      <span class="status-badge status-success">Ready</span>
      <section class="panel onboarding-card">
        <h2>Connect in three steps</h2>
        <ol class="onboarding-list"><li><span>1</span><div><strong>Create</strong><small>Register the callback.</small></div></li></ol>
        <a class="button button-secondary button-wide" href="#onboarding">Create now</a>
      </section>
      <p class="muted" id="tertiary">Supporting information</p>
    </div>
  </main>
</body>
</html>"""
url = "data:text/html;base64," + base64.b64encode(html.encode("utf-8")).decode("ascii")
failures = []


def check(condition, message):
    if not condition:
        failures.append(message)


new_tab(url)
wait_for_load()
try:
    cdp(
        "Emulation.setDeviceMetricsOverride",
        width=1200,
        height=900,
        deviceScaleFactor=1,
        mobile=False,
        screenWidth=1200,
        screenHeight=900,
    )
    desktop = js(
        """(() => {
          const style = selector => getComputedStyle(document.querySelector(selector));
          const body = style('body');
          const header = document.querySelector('.topbar-inner').getBoundingClientRect();
          const primary = style('.button');
          const brand = style('.brand-mark');
          const panel = style('.panel');
          const badge = style('.status-success');
          const dot = getComputedStyle(document.querySelector('.status-success'), '::before');
          return {
            pageBackground: body.backgroundColor,
            pageBackgroundImage: body.backgroundImage,
            pageText: body.color,
            headerHeight: header.height,
            primaryBackground: primary.backgroundColor,
            primaryBackgroundImage: primary.backgroundImage,
            primaryRadius: primary.borderRadius,
            brandBackgroundImage: brand.backgroundImage,
            panelBorderWidth: panel.borderTopWidth,
            panelRadius: panel.borderRadius,
            panelShadow: panel.boxShadow,
            badgeBackground: badge.backgroundColor,
            badgeText: badge.color,
            badgeDot: dot.backgroundColor
          };
        })()"""
    )
    check(desktop["pageBackground"] == "rgb(250, 250, 250)", "page canvas must use Vercel #FAFAFA")
    check(desktop["pageBackgroundImage"] == "none", "page canvas must not use decorative gradients")
    check(desktop["pageText"] == "rgb(23, 23, 23)", "primary text must use Vercel #171717")
    check(desktop["headerHeight"] == 64, f"desktop header is {desktop['headerHeight']:.1f}px, want 64px")
    check(desktop["primaryBackground"] == "rgb(23, 23, 23)", "primary button must use Vercel black")
    check(desktop["primaryBackgroundImage"] == "none", "primary button must not use a gradient")
    check(desktop["primaryRadius"] == "6px", "default controls must use a 6px radius")
    check(desktop["brandBackgroundImage"] == "none", "brand mark must not use a gradient")
    check(desktop["panelBorderWidth"] == "0px", "panels must use shadow-as-border without a CSS border")
    check(desktop["panelRadius"] == "12px", "elevated panels must use a 12px radius")
    check(desktop["panelShadow"] != "none", "panels must retain a shadow-as-border boundary")
    check(desktop["badgeBackground"] == "rgba(0, 0, 0, 0)", "status labels must not use colored fills")
    check(desktop["badgeText"] == "rgb(77, 77, 77)", "status labels must use neutral text")
    check(desktop["badgeDot"] == "rgb(57, 142, 74)", "success status must keep color only in its dot")

    cdp(
        "Emulation.setDeviceMetricsOverride",
        width=768,
        height=800,
        deviceScaleFactor=1,
        mobile=False,
        screenWidth=768,
        screenHeight=800,
    )
    header = js(
        """(() => {
          const box = selector => document.querySelector(selector).getBoundingClientRect();
          const brand = box('.portal-brand');
          const nav = box('.portal-nav');
          const role = box('.role-label');
          const logout = document.querySelector('.logout-button');
          return {
            brandBottom: brand.bottom,
            navTop: nav.top,
            roleHeight: role.height,
            logoutWhiteSpace: getComputedStyle(logout).whiteSpace
          };
        })()"""
    )
    check(header["navTop"] >= header["brandBottom"], "768px header navigation must move to its own row")
    check(header["roleHeight"] <= 24, "768px role label must stay on one line")
    check(header["logoutWhiteSpace"] == "nowrap", "768px sign-out control must not wrap")

    active_focused = False
    for _ in range(4):
        press_key("Tab")
        active_focused = js("document.activeElement.classList.contains('is-active')")
        if active_focused:
            break
    check(active_focused, "keyboard focus must reach the active navigation item")
    if active_focused:
        active_shadow = js("getComputedStyle(document.activeElement).boxShadow")
        check(
            "rgb(0, 114, 245)" in active_shadow and "4px" in active_shadow and "2px" in active_shadow,
            "keyboard focus must use Vercel's blue double-ring pattern",
        )

    cdp(
        "Emulation.setDeviceMetricsOverride",
        width=390,
        height=844,
        deviceScaleFactor=1,
        mobile=True,
        screenWidth=390,
        screenHeight=844,
    )
    mobile = js(
        """(() => {
          const height = selector => document.querySelector(selector).getBoundingClientRect().height;
          const panel = document.querySelector('.onboarding-card').getBoundingClientRect();
          const heading = document.querySelector('.onboarding-card h2').getBoundingClientRect();
          return {
            onboardingInset: heading.left - panel.left,
            nav: height('.portal-nav a'),
            logout: height('.logout-button'),
            button: height('.button'),
            toolbarLink: height('.toolbar-actions .text-link'),
            rowAction: height('.row-action')
          };
        })()"""
    )
    check(mobile["onboardingInset"] >= 16, "onboarding content must be inset from the panel border")
    for control in ["nav", "logout", "button", "toolbarLink", "rowAction"]:
        check(mobile[control] >= 44, f"mobile {control} target is {mobile[control]:.1f}px, want at least 44px")

    contrast = js(
        """(() => {
          const parseHex = value => {
            const hex = value.trim().slice(1);
            return [0, 2, 4].map(index => parseInt(hex.slice(index, index + 2), 16));
          };
          const luminance = rgb => {
            const linear = rgb.map(value => {
              value /= 255;
              return value <= 0.04045 ? value / 12.92 : Math.pow((value + 0.055) / 1.055, 2.4);
            });
            return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
          };
          const ratio = (foreground, background) => {
            const first = luminance(parseHex(foreground));
            const second = luminance(parseHex(background));
            return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
          };
          const styles = getComputedStyle(document.documentElement);
          const tertiary = styles.getPropertyValue('--text-3');
          return {
            onSurface: ratio(tertiary, styles.getPropertyValue('--surface')),
            onPage: ratio(tertiary, styles.getPropertyValue('--bg'))
          };
        })()"""
    )
    check(contrast["onSurface"] >= 4.5, f"tertiary text contrast on panels is {contrast['onSurface']:.2f}, want 4.5")
    check(contrast["onPage"] >= 4.5, f"tertiary text contrast on the page is {contrast['onPage']:.2f}, want 4.5")
finally:
    close_tab()

if failures:
    raise AssertionError("Portal style regressions:\n- " + "\n- ".join(failures))

print("portal style checks passed")
