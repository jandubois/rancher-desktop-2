import type { Page, Locator } from '@playwright/test';

export class ContainerEngineNav {
  readonly page:                  Page;
  readonly nav:                   Locator;
  readonly tabGeneral:            Locator;
  readonly tabAllowedImages:      Locator;
  readonly engineSelector:        Locator;
  readonly engineMoby:            Locator;
  readonly engineContainerd:      Locator;
  readonly allowedImages:         Locator;
  readonly allowedImagesCheckbox: Locator;
  readonly enabledLockedField:    Locator;

  constructor(page: Page) {
    this.page = page;
    this.nav = page.getByTestId('nav-container-engine');
    this.tabGeneral = page.getByTestId('preferences-body').getByRole('tablist').getByTestId('btn-general');
    this.tabAllowedImages = page.getByTestId('preferences-body').getByRole('tablist').getByTestId('btn-allowed-images');
    this.engineSelector = page.getByTestId('containerEngine').locator('.engine-selector');
    this.engineMoby = this.engineSelector.locator('input[name="containerEngine"][value="moby"] + [role="radio"]');
    this.engineContainerd = this.engineSelector.locator('input[name="containerEngine"][value="containerd"] + [role="radio"]');
    this.allowedImages = page.locator('[data-test="allowedImages"]');
    this.allowedImagesCheckbox = page.getByTestId('allowedImagesCheckbox');
    this.enabledLockedField = this.allowedImagesCheckbox.locator('.icon-lock');
  }
}
