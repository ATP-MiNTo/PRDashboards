import { fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { renderWithClient } from '@/test-utils';
import { ClientForm } from './ClientForm';
import { PatForm } from './PatForm';

const api = vi.hoisted(() => ({
  putGithubPat: vi.fn(),
  putSetupClient: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockReset());
});

const bad = () => new ApiError(400, 'invalid_token');

describe('PatForm', () => {
  it('marks the field invalid and points it at the alert after a failed save', async () => {
    api.putGithubPat.mockRejectedValue(bad());
    renderWithClient(<PatForm />);
    const field = screen.getByLabelText('Personal access token');
    expect(field).not.toHaveAttribute('aria-invalid');
    expect(field).not.toHaveAttribute('aria-describedby');
    fireEvent.change(field, { target: { value: 'ghp_nope' } });
    fireEvent.click(screen.getByRole('button', { name: 'Validate and save' }));
    const alert = await screen.findByRole('alert');
    expect(field).toHaveAttribute('aria-invalid', 'true');
    expect(field).toHaveAttribute('aria-describedby', alert.id);
    expect(field).toHaveAccessibleDescription(alert.textContent ?? '');
  });
});

describe('ClientForm', () => {
  it('marks every field invalid and describes it by the alert after a failed save', async () => {
    api.putSetupClient.mockRejectedValue(bad());
    renderWithClient(
      <ClientForm
        provider="github"
        title="GitHub App"
        configured={false}
        callbackUrl="x"
        withSlug
      />,
    );
    const fields = [
      screen.getByLabelText('Client ID'),
      screen.getByLabelText('Client secret'),
      screen.getByLabelText(/App slug/),
    ];
    for (const f of fields) expect(f).not.toHaveAttribute('aria-invalid');
    fireEvent.change(fields[0]!, { target: { value: 'id' } });
    fireEvent.change(fields[1]!, { target: { value: 'secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save credentials' }));
    const alert = await screen.findByRole('alert');
    await waitFor(() => expect(fields[0]).toHaveAttribute('aria-invalid', 'true'));
    for (const f of fields) {
      expect(f).toHaveAttribute('aria-invalid', 'true');
      expect(f).toHaveAttribute('aria-describedby', alert.id);
    }
  });
});
