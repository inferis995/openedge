import { useAuthStore } from '@/stores/useAuthStore';
import { showApiError } from '@/lib/api-error-handler';
import { useState, useEffect } from 'react';
import { useQuery, useMutation } from '@tanstack/react-query';
import { User, KeyRound, CheckCircle2, Eye, EyeOff, ShieldCheck, ShieldOff, QrCode } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { profileApi } from '@/api/profile';
import apiClient from '@/api/client';
import { useTranslation } from 'react-i18next';

export default function ProfilePage() {
    const { t } = useTranslation();
    const { data: profile, isLoading } = useQuery({
        queryKey: ['profile'],
        queryFn: profileApi.get,
    });

    const [oldPw, setOldPw] = useState('');
    const [newPw, setNewPw] = useState('');
    const [confirmPw, setConfirmPw] = useState('');
    const [showOld, setShowOld] = useState(false);
    const [showNew, setShowNew] = useState(false);
    const [pwError, setPwError] = useState('');
    const [pwSuccess, setPwSuccess] = useState(false);

    const changePw = useMutation({
        mutationFn: () => profileApi.changePassword(oldPw, newPw),
        onSuccess: (res) => {
            // The change retires every session of this account, this one
            // included: carry on with the token the server hands back.
            const { user, login } = useAuthStore.getState();
            if (res.token && user) login(res.token, user);
            setPwSuccess(true);
            setOldPw(''); setNewPw(''); setConfirmPw('');
            setTimeout(() => setPwSuccess(false), 3000);
        },
        onError: (err: unknown) => {
            const axiosErr = err as { response?: { data?: { error?: string } } };
            setPwError(axiosErr.response?.data?.error ?? t('profilePage.pw_failed'));
        },
    });

    const handleChangePw = (e: React.FormEvent) => {
        e.preventDefault();
        setPwError('');
        if (newPw !== confirmPw) { setPwError(t('profilePage.pw_mismatch')); return; }
        if (newPw.length < 12) { setPwError(t('profilePage.pw_too_short')); return; }
        changePw.mutate();
    };

    // MFA state
    const [mfaEnabled, setMfaEnabled] = useState(false);
    const [mfaSetup, setMfaSetup] = useState<{ secret: string; qr_url: string; qr_png?: string } | null>(null);
    const [mfaCode, setMfaCode] = useState('');
    const [mfaMsg, setMfaMsg] = useState('');
    const [disablePw, setDisablePw] = useState('');

    useEffect(() => {
        apiClient.get('/auth/me/mfa/status').then((r: { data: { mfa_enabled: boolean } }) => setMfaEnabled(r.data.mfa_enabled)).catch(() => {});
    }, []);

    const startSetup = async () => {
        setMfaMsg('');
        try {
            const r = await apiClient.post('/auth/me/mfa/setup');
            setMfaSetup(r.data);
        } catch (e) {
            // Used to reject unhandled: the button did nothing, silently.
            showApiError(e);
        }
    };

    const enableMFA = async () => {
        try {
            await apiClient.post('/auth/me/mfa/enable', { code: mfaCode });
            setMfaEnabled(true);
            setMfaSetup(null);
            setMfaCode('');
            setMfaMsg('profilePage.mfa_enabled_ok');
        } catch {
            setMfaMsg('profilePage.mfa_bad_code');
        }
    };

    const disableMFA = async () => {
        try {
            await apiClient.delete('/auth/me/mfa/disable', { data: { password: disablePw } });
            setMfaEnabled(false);
            setDisablePw('');
            setMfaMsg('profilePage.mfa_disabled_ok');
        } catch {
            setMfaMsg('profilePage.mfa_bad_password');
        }
    };

    if (isLoading) return <div className="p-6 text-muted-foreground">{t('common.loading')}</div>;

    return (
        <div className="p-6 max-w-2xl space-y-8">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
                    <User className="h-5 w-5 text-primary" />
                </div>
                <div>
                    <h1 className="text-2xl font-bold">{t('profilePage.title')}</h1>
                    <p className="text-sm text-muted-foreground">{t('profilePage.subtitle')}</p>
                </div>
            </div>

            {/* Profile info */}
            <div className="rounded-xl border bg-card p-6 space-y-4">
                <h2 className="font-semibold text-sm uppercase tracking-wide text-muted-foreground">{t('profilePage.account')}</h2>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
                    <div>
                        <p className="text-muted-foreground mb-0.5">{t('profilePage.username')}</p>
                        <p className="font-medium">{profile?.username}</p>
                    </div>
                    <div>
                        <p className="text-muted-foreground mb-0.5">{t('profilePage.role')}</p>
                        <Badge variant={profile?.role === 'admin' ? 'default' : 'secondary'}>
                            {profile?.role === 'admin' ? t('nav.admin_role') : profile?.role === 'user' ? t('nav.user_role') : profile?.role}
                        </Badge>
                    </div>
                    <div>
                        <p className="text-muted-foreground mb-0.5">{t('profilePage.full_name')}</p>
                        <p className="font-medium">{profile?.full_name || <span className="text-muted-foreground italic">{t('profilePage.not_set')}</span>}</p>
                    </div>
                    <div>
                        <p className="text-muted-foreground mb-0.5">{t('profilePage.email')}</p>
                        <p className="font-medium">{profile?.email || <span className="text-muted-foreground italic">{t('profilePage.not_set')}</span>}</p>
                    </div>
                </div>
            </div>

            {/* Change password */}
            <div className="rounded-xl border bg-card p-6 space-y-4">
                <div className="flex items-center gap-2">
                    <KeyRound className="h-4 w-4 text-muted-foreground" />
                    <h2 className="font-semibold text-sm uppercase tracking-wide text-muted-foreground">{t('profilePage.change_password')}</h2>
                </div>

                <form onSubmit={handleChangePw} className="space-y-4">
                    <div className="space-y-1.5">
                        <Label htmlFor="old-pw">{t('profilePage.current_password')}</Label>
                        <div className="relative">
                            <Input
                                id="old-pw"
                                type={showOld ? 'text' : 'password'}
                                value={oldPw}
                                onChange={e => setOldPw(e.target.value)}
                                required
                                placeholder={t('profilePage.current_password_placeholder')}
                            />
                            <button type="button" onClick={() => setShowOld(v => !v)}
                                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground" tabIndex={-1}>
                                {showOld ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                            </button>
                        </div>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="new-pw">{t('profilePage.new_password')}</Label>
                        <div className="relative">
                            <Input
                                id="new-pw"
                                type={showNew ? 'text' : 'password'}
                                value={newPw}
                                onChange={e => setNewPw(e.target.value)}
                                required
                                minLength={12}
                                placeholder={t('profilePage.new_password_placeholder')}
                            />
                            <button type="button" onClick={() => setShowNew(v => !v)}
                                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground" tabIndex={-1}>
                                {showNew ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                            </button>
                        </div>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="confirm-pw">{t('profilePage.confirm_password')}</Label>
                        <Input
                            id="confirm-pw"
                            type="password"
                            value={confirmPw}
                            onChange={e => setConfirmPw(e.target.value)}
                            required
                            placeholder={t('profilePage.confirm_placeholder')}
                        />
                    </div>

                    {pwError && <p className="text-sm text-destructive">{pwError}</p>}
                    {pwSuccess && (
                        <div className="flex items-center gap-2 text-green-600 dark:text-green-400 text-sm">
                            <CheckCircle2 className="h-4 w-4" /> {t('profilePage.pw_updated')}
                        </div>
                    )}

                    <Button type="submit" disabled={changePw.isPending || !oldPw || !newPw || !confirmPw}>
                        {changePw.isPending ? t('profilePage.updating') : t('profilePage.update_password')}
                    </Button>
                </form>
            </div>

            {/* MFA Section */}
            <div className="rounded-xl border bg-card p-6 space-y-4">
                <div className="flex items-center gap-3">
                    <h2 className="font-semibold text-sm uppercase tracking-wide text-muted-foreground flex-1">{t('profilePage.mfa_title')}</h2>
                    <Badge variant={mfaEnabled ? 'default' : 'secondary'}>
                        {mfaEnabled ? t('profilePage.mfa_on') : t('profilePage.mfa_off')}
                    </Badge>
                </div>

                {mfaMsg && (
                    <p className={`text-sm ${mfaMsg.endsWith('_ok') ? 'text-green-600 dark:text-green-400' : 'text-destructive'}`}>{t(mfaMsg)}</p>
                )}

                {!mfaEnabled && !mfaSetup && (
                    <div className="space-y-3">
                        <p className="text-sm text-muted-foreground">{t('profilePage.mfa_intro')}</p>
                        <Button variant="outline" className="gap-2" onClick={startSetup}>
                            <QrCode className="h-4 w-4" /> {t('profilePage.mfa_setup')}
                        </Button>
                    </div>
                )}

                {mfaSetup && !mfaEnabled && (
                    <div className="space-y-4">
                        <p className="text-sm text-muted-foreground">{t('profilePage.mfa_scan')}</p>
                        {/* Drawn by the server. It used to come from
                            api.qrserver.com, with the secret in the URL. */}
                        {mfaSetup.qr_png && (
                            <div className="flex justify-center">
                                <img
                                    src={mfaSetup.qr_png}
                                    alt={t('profilePage.mfa_qr_alt')}
                                    className="rounded-lg border border-border bg-white p-2"
                                    width={200} height={200}
                                />
                            </div>
                        )}
                        <div className="grid gap-1">
                            <Label className="text-xs">{t('profilePage.mfa_manual_key')}</Label>
                            <code className="text-xs bg-muted rounded px-3 py-2 font-mono break-all select-all">{mfaSetup.secret}</code>
                        </div>
                        <div className="flex gap-2">
                            <Input
                                type="text"
                                inputMode="numeric"
                                maxLength={6}
                                placeholder={t('profilePage.mfa_code_placeholder')}
                                value={mfaCode}
                                onChange={e => setMfaCode(e.target.value.replace(/\D/g, ''))}
                                className="font-mono text-center tracking-widest"
                                autoComplete="one-time-code"
                            />
                            <Button onClick={enableMFA} disabled={mfaCode.length !== 6} className="gap-2">
                                <ShieldCheck className="h-4 w-4" /> {t('profilePage.mfa_enable')}
                            </Button>
                        </div>
                        <button className="text-xs text-muted-foreground hover:text-foreground" onClick={() => { setMfaSetup(null); setMfaCode(''); }}>{t('common.cancel')}</button>
                    </div>
                )}

                {mfaEnabled && (
                    <div className="space-y-3">
                        <div className="flex items-center gap-2 text-green-600 dark:text-green-400 text-sm">
                            <ShieldCheck className="h-4 w-4" /> {t('profilePage.mfa_protected')}
                        </div>
                        <p className="text-sm text-muted-foreground">{t('profilePage.mfa_disable_hint')}</p>
                        <div className="flex gap-2">
                            <Input type="password" placeholder={t('profilePage.current_password')} value={disablePw} onChange={e => setDisablePw(e.target.value)} />
                            <Button variant="destructive" onClick={disableMFA} disabled={!disablePw} className="gap-2 shrink-0">
                                <ShieldOff className="h-4 w-4" /> {t('profilePage.mfa_disable')}
                            </Button>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}
