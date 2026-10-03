import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useAuthStore } from '@/stores/useAuthStore';
import ForcedPasswordChange from '@/components/ForcedPasswordChange';

const RequireAuth = () => {
    const { isAuthenticated, user } = useAuthStore();
    const location = useLocation();

    if (!isAuthenticated()) {
        return <Navigate to="/login" state={{ from: location }} replace />;
    }
    // Signed in with the default password: the server refuses everything
    // else until it is changed, so ask for that before showing any page.
    if (user?.must_change_password) {
        return <ForcedPasswordChange />;
    }

    return <Outlet />;
};

export default RequireAuth;
