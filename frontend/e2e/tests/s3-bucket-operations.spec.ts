import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, type Page, test } from '@playwright/test'

test.describe
  .serial('S3 Bucket Operations', () => {
    async function loginToS3({ page }: { page: Page }) {
      await page.goto('/object-storage/s3-bucket')
      await page.getByRole('combobox').click()
      await page.getByRole('option', { name: process.env.VITE_REGION }).click()
      await page
        .locator('[data-test="access_key_input"]')
        .fill(process.env.VITE_ACCESS_KEY || '')
      await page
        .locator('[data-test="secret_key_input"]')
        .fill(process.env.VITE_SECRET_KEY || '')
      await page.getByRole('button', { name: 'Submit' }).click()
      await expect(page).toHaveURL('/object-storage/s3-bucket/buckets')
    }

    async function createTestBucket(
      page: Page,
      prefix: string = 'test-bucket'
    ) {
      const bucketName = prefix

      await page.getByRole('button', { name: 'Create Bucket' }).click()
      await expect(page.getByRole('dialog')).toBeVisible()
      await page.locator('[data-test="bucket-name-input"]').fill(bucketName)
      await page.getByRole('button', { name: 'Create' }).click()
      await expect(
        page
          .locator('[data-test="toast-title"]')
          .filter({ hasText: 'Bucket created successfully!' })
      ).toBeVisible()

      return bucketName
    }

    const bucketName = `e2e-automate-bucket-${Date.now()}`

    test.beforeEach(async ({ page }) => {
      await loginToS3({ page })
    })

    test('should create a new bucket successfully', async ({ page }) => {
      const createdBucketName = await createTestBucket(page, bucketName)

      // Verify bucket appears in the list
      await expect(
        page.locator(
          `[data-test="bucket-card"][data-test-bucket-name="${createdBucketName}"]`
        )
      ).toBeVisible()
    })

    test('should upload an object to bucket successfully', async ({ page }) => {
      // Wait for the bucket card to be visible and click its Browse button
      await page
        .locator(
          `[data-test="bucket-card"][data-test-bucket-name="${bucketName}"]`
        )
        .locator('[data-test="browse-bucket-button"]')
        .click()

      // Verify we're on the correct page
      await expect(page).toHaveURL(
        `/object-storage/s3-bucket/buckets/${bucketName}`
      )

      // Create a test file to upload
      const testFilePath = path.join(
        path.dirname(fileURLToPath(import.meta.url)),
        'test-file.txt'
      )

      // Set up file input handling
      const fileChooserPromise = page.waitForEvent('filechooser')

      await page.locator('[data-test="file-upload-button"]').click()

      const fileChooser = await fileChooserPromise

      await fileChooser.setFiles([testFilePath])

      // Verify file appears in the object list
      await expect(page.getByText('test-file.txt')).toBeVisible()
    })

    test('should not delete a bucket with objects', async ({ page }) => {
      const bucketCard = page.locator(
        `[data-test="bucket-card"][data-test-bucket-name="${bucketName}"]`
      )

      await bucketCard
        .locator('[data-test="bucket-more-details"]')
        .locator('[data-test="bucket-more-details-trigger"]')
        .click()

      await page.locator('[data-test="delete-bucket-button"]').click()

      const dialog = page.getByRole('dialog')

      await expect(dialog).toBeVisible()

      await page.locator('[data-test="confirm-delete-button"]').click()

      const toast = page
        .locator('[data-test="toast-title"]')
        .filter({ hasText: 'Failed to delete bucket' })

      await expect(toast).toBeVisible({ timeout: 5000 })

      await page.locator('[data-test="cancel-delete-button"]').click()
      await expect(dialog).toBeHidden({ timeout: 5000 })

      // An error toast stays until dismissed.
      await page.locator('[data-test="toast-close"]').click()
      await expect(toast).toBeHidden({ timeout: 5000 })

      await bucketCard.scrollIntoViewIfNeeded()
      await expect(bucketCard).toBeVisible({ timeout: 5000 })
    })

    test('should delete bucket object successfully', async ({ page }) => {
      const bucketCard = page.locator(
        `[data-test="bucket-card"][data-test-bucket-name="${bucketName}"]`
      )

      await bucketCard.locator('[data-test="browse-bucket-button"]').click()

      await page.waitForURL(`**/object-storage/s3-bucket/buckets/${bucketName}`)

      await page.waitForResponse(
        response =>
          response.url().includes('/api/object/list') &&
          response.status() === 200
      )

      await page.click('button:has(svg.lucide-trash2)')

      await expect(page.getByRole('dialog')).toBeVisible()
      await page.locator('[data-test="confirm-delete-button"]').click()

      await expect(
        page
          .locator('[data-test="toast-title"]')
          .filter({ hasText: 'Object deleted successfully' })
      ).toBeVisible()
    })

    test('should delete a bucket successfully', async ({ page }) => {
      // Wait for the bucket card to be visible
      const bucketCard = page.locator(
        `[data-test="bucket-card"][data-test-bucket-name="${bucketName}"]`
      )

      // Click the more details trigger button and then the delete button
      await bucketCard
        .locator('[data-test="bucket-more-details"]')
        .locator('[data-test="bucket-more-details-trigger"]')
        .click()
      await page.locator('[data-test="delete-bucket-button"]').click()

      // Wait for confirmation dialog and confirm deletion
      await expect(page.getByRole('dialog')).toBeVisible()
      await page.locator('[data-test="confirm-delete-button"]').click()

      // Verify bucket no longer appears in the list
      await expect(
        page.locator(
          `[data-test="bucket-card"][data-test-bucket-name="${bucketName}"]`
        )
      ).not.toBeVisible()
    })
  })
