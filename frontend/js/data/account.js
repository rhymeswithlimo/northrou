// Account/session seam.
//
//   getMe()   -> GET  /api/me      {account:{email}, profile, profiles[], admin}
//   signOut() -> POST /api/auth/logout {refresh_token}
//
// Note on `admin`: it means the CURRENT TOKEN IS ALREADY ELEVATED, not "this
// profile may administer". Every profile may administer, because admin is
// gated on an emailed OTP rather than identity. So the Server Admin section is
// shown to everyone; `admin` only decides whether to skip the OTP prompt.

export async function getMe() {
    return {
        account: { email: 'you@example.com' },
        profile: { id: 1, name: 'Tomas' },
        profiles: [
            { id: 1, name: 'Tomas' },
            { id: 2, name: 'Kira' },
            { id: 3, name: 'Ivan' },
            { id: 4, name: 'Lena' },
        ],
        admin: false,
    };
}

export async function signOut() {
    window.location.assign('login.html');
}
