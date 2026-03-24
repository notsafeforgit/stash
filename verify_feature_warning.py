import sys
from playwright.sync_api import sync_playwright

def verify():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        print("Navigating to scenes page (PORT 3000 FOR FRONTEND DEV SERVER)")
        # Notice we are using port 3000 now, which connects to the Vite/Webpack dev server!
        page.goto("http://localhost:3000/scenes?sort=date&dir=desc&q=foo")
        page.wait_for_load_state("networkidle")
        page.wait_for_timeout(4000) # extra wait for dev server

        print("Closing any modals...")
        try:
            close_btn = page.locator('.modal-footer button:has-text("Close")')
            if close_btn.is_visible():
                close_btn.click()
                print("Closed modal")
                page.wait_for_timeout(1000)
        except:
            pass

        print("Clicking first item to activate multi-select...")
        page.evaluate("() => document.querySelector('.card-check').click()")
        page.wait_for_timeout(1000)

        print("Finding multi select buttons via JS click to avoid visibility issues")
        try:
            page.evaluate("() => document.querySelector('button[title=\"Select All\"]').click()")
            print("Clicked Select All via JS")
        except:
            print("Failed to click Select All via JS")

        page.wait_for_timeout(1000)

        print("Finding Edit button via JS click")
        try:
            page.evaluate("""() => {
                const btn = document.querySelector('.edit-existing-button') ||
                            Array.from(document.querySelectorAll('button')).find(b => b.innerHTML.includes('fa-pencil-alt'));
                if (btn) btn.click();
            }""")
            print("Clicked edit button via JS")
        except:
            print("Failed to click Edit button via JS")

        print("Waiting for modal...")
        try:
            page.wait_for_selector('.modal-content', state='visible', timeout=5000)
            print("Modal appeared!")
        except:
            print("Modal did NOT appear! Saving failure screenshot.")
            page.screenshot(path="/home/jules/verification/verification_failure.png")
            return 1

        page.wait_for_timeout(1000)

        # Check if the checkbox is visible
        apply_to_all = page.locator('#apply-to-all-checkbox')
        if apply_to_all.is_visible():
            print("Apply to all checkbox found!")

            # Take screenshot before checking
            page.screenshot(path="/home/jules/verification/verification_warning_uncheck.png")

            apply_to_all.click()
            page.wait_for_timeout(1000)

            # Take screenshot after checking
            page.screenshot(path="/home/jules/verification/verification_warning.png")
            print("Screenshot saved to /home/jules/verification/verification_warning.png")
            return 0
        else:
            print("ERROR: Apply to all checkbox not visible!")
            page.screenshot(path="/home/jules/verification/verification_failure.png")
            return 1

if __name__ == "__main__":
    try:
        sys.exit(verify())
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)
