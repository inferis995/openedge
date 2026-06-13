import { useState } from 'react';
import { useQuery, useMutation } from '@tanstack/react-query';
import { User, KeyRound, CheckCircle2, Eye, EyeOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { profileApi } from '@/api/profile';

export default function ProfilePage() {
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
        onSuccess: () => {
            setPwSuccess(true);
            setOldPw(''); setNewPw(''); setConfirmPw('');
            setTimeout(() => setPwSuccess(false), 3000);
        },
        onError: (err: unknown) => {
            const axiosErr = err as { response?: { data?: { error?: string } } };
            setPwError(axiosErr.response?.data?.error ?? 'Failed to update password.');
        },
    });

    const handleChangePw = (e: React.FormEvent) => {
        e.preventDefault();
        setPwError('');
        if (newPw !== confirmPw) { setPwError('New passwords do not match.'); return; }
        if (newPw.length < 6) { setPwError('New password must be at least 6 characters.'); return; }
        changePw.mutate();
    };

    if (isLoading) return <div className="p-6 text-muted-foreground">Loading…</div>;

    return (
        <div className="p-6 max-w-2xl space-y-8">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
                    <User className="h-5 w-5 text-primary" />
                </div>
                <div>
                    <h1 className="text-2xl font-bold">My Profile</h1>
                    <p className="text-sm text-muted-foreground">Manage your account settings</p>
                </div>
            </div>

            {/* Profile info */}
            <div className="rounded-xl border bg-card p-6 space-y-4">
                <h2 className="font-semibold text-sm uppercase tracking-wide text-muted-foreground">Account</h2>
                <div className="grid grid-cols-2 gap-4 text-sm">
                    <div>
                        <p className="text-muted-foreground mb-0.5">Username</p>
                        <p className="font-medium">{profile?.username}</p>
                    </div>
                    <div>
                        <p className="text-muted-foreground mb-0.5">Role</p>
                        <Badge variant={profile?.role === 'admin' ? 'default' : 'secondary'}>
                            {profile?.role}
                        </Badge>
                    </div>
                    <div>
                        <p className="text-muted-foreground mb-0.5">Full name</p>
                        <p className="font-medium">{profile?.full_name || <span className="text-muted-foreground italic">not set</span>}</p>
                    </div>
                    <div>
                        <p className="text-muted-foreground mb-0.5">Email</p>
                        <p className="font-medium">{profile?.email || <span className="text-muted-foreground italic">not set</span>}</p>
                    </div>
                </div>
            </div>

            {/* Change password */}
            <div className="rounded-xl border bg-card p-6 space-y-4">
                <div className="flex items-center gap-2">
                    <KeyRound className="h-4 w-4 text-muted-foreground" />
                    <h2 className="font-semibold text-sm uppercase tracking-wide text-muted-foreground">Change Password</h2>
                </div>

                <form onSubmit={handleChangePw} className="space-y-4">
                    <div className="space-y-1.5">
                        <Label htmlFor="old-pw">Current password</Label>
                        <div className="relative">
                            <Input
                                id="old-pw"
                                type={showOld ? 'text' : 'password'}
                                value={oldPw}
                                onChange={e => setOldPw(e.target.value)}
                                required
                                placeholder="Enter current password"
                            />
                            <button type="button" onClick={() => setShowOld(v => !v)}
                                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground" tabIndex={-1}>
                                {showOld ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                            </button>
                        </div>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="new-pw">New password</Label>
                        <div className="relative">
                            <Input
                                id="new-pw"
                                type={showNew ? 'text' : 'password'}
                                value={newPw}
                                onChange={e => setNewPw(e.target.value)}
                                required
                                minLength={6}
                                placeholder="Min. 6 characters"
                            />
                            <button type="button" onClick={() => setShowNew(v => !v)}
                                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground" tabIndex={-1}>
                                {showNew ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                            </button>
                        </div>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="confirm-pw">Confirm new password</Label>
                        <Input
                            id="confirm-pw"
                            type="password"
                            value={confirmPw}
                            onChange={e => setConfirmPw(e.target.value)}
                            required
                            placeholder="Repeat new password"
                        />
                    </div>

                    {pwError && <p className="text-sm text-destructive">{pwError}</p>}
                    {pwSuccess && (
                        <div className="flex items-center gap-2 text-green-600 dark:text-green-400 text-sm">
                            <CheckCircle2 className="h-4 w-4" /> Password updated successfully!
                        </div>
                    )}

                    <Button type="submit" disabled={changePw.isPending || !oldPw || !newPw || !confirmPw}>
                        {changePw.isPending ? 'Updating…' : 'Update password'}
                    </Button>
                </form>
            </div>
        </div>
    );
}
