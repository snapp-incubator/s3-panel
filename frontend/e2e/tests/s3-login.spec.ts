import { expect, test } from '@playwright/test'

test.describe('S3 Bucket Login Form', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/object-storage/s3-bucket')
  })

  test('should fill form with test credentials and submit successfully', async ({
    page
  }) => {
    // Verify we're on the login page
    await expect(page).toHaveURL('/object-storage/s3-bucket')
    await expect(page.locator('h1')).toHaveText('S3 Bucket')
    // Fill out the form
    // Select region from dropdown
    await page.getByRole('combobox').click()
    await page.getByRole('option', { name: process.env.VITE_REGION }).click()

    // Fill access key
    await page
      .locator('[data-test="access_key_input"]')
      .fill(process.env.VITE_ACCESS_KEY || '')

    // Fill secret key
    await page
      .locator('[data-test="secret_key_input"]')
      .fill(process.env.VITE_SECRET_KEY || '')

    // Submit the form
    await page.getByRole('button', { name: 'Submit' }).click()

    // Verify redirect to buckets page
    await expect(page).toHaveURL('/object-storage/s3-bucket/buckets')

    // Verify buckets page elements are present
    await expect(
      page.getByRole('button', { name: 'Create Bucket' })
    ).toBeVisible()
  })

  test('should show validation errors for empty fields', async ({ page }) => {
    // Click submit without filling any fields
    await page.getByRole('button', { name: 'Submit' }).click()

    // Verify validation error messages using FormMessage components
    await expect(
      page.getByTestId('form-error-message-access_key')
    ).toBeVisible()
    await expect(
      page.getByTestId('form-error-message-secret_key')
    ).toBeVisible()
  })

  test('should handle invalid credentials', async ({ page }) => {
    // Fill out the form with invalid credentials
    await page.getByRole('combobox').click()
    await page.getByRole('option', { name: 'Teh 2' }).click()
    await page
      .locator('[data-test="access_key_input"]')
      .fill('invalid_access_key')
    await page
      .locator('[data-test="secret_key_input"]')
      .fill('invalid_secret_key')

    // Submit the form
    await page.getByRole('button', { name: 'Submit' }).click()

    // Verify we stay on the login page
    await expect(page).toHaveURL('/object-storage/s3-bucket')

    // Look for the error message in the toast notification content div
    await expect(
      page
        .locator('div.text-sm.font-semibold')
        .filter({ hasText: 'invalid AccessKey' })
    ).toBeVisible()
  })
})
