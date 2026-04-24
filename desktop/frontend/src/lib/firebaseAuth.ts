import { getApp, getApps, initializeApp } from 'firebase/app';
import {
  GithubAuthProvider,
  GoogleAuthProvider,
  browserLocalPersistence,
  getAuth,
  setPersistence,
  signInWithPopup,
  signOut,
} from 'firebase/auth';

export type WindsurfOAuthProvider = 'google' | 'github';

export type WindsurfOAuthLoginResult = {
  email: string;
  refreshToken: string;
};

const firebaseConfig = {
  apiKey: 'AIzaSyDsOl-1XpT5err0Tcnx8FFod1H8gVGIycY',
  authDomain: 'exa2-fb170.firebaseapp.com',
  projectId: 'exa2-fb170',
  appId: '1:957777847521:web:390f31e87633dc5cc803a0',
};

const firebaseApp = getApps().length > 0 ? getApp() : initializeApp(firebaseConfig);
const auth = getAuth(firebaseApp);

function createProvider(provider: WindsurfOAuthProvider) {
  if (provider === 'google') {
    const google = new GoogleAuthProvider();
    google.setCustomParameters({ prompt: 'select_account' });
    return google;
  }

  const github = new GithubAuthProvider();
  github.addScope('user:email');
  github.setCustomParameters({ allow_signup: 'true' });
  return github;
}

export async function loginWithWindsurfOAuth(provider: WindsurfOAuthProvider): Promise<WindsurfOAuthLoginResult> {
  await setPersistence(auth, browserLocalPersistence);
  const credential = await signInWithPopup(auth, createProvider(provider));
  const refreshToken = credential.user.refreshToken || '';
  const email = credential.user.email || '';

  if (!refreshToken) {
    throw new Error('OAuth 登录成功，但没有拿到 refresh token');
  }

  await signOut(auth).catch(() => undefined);

  return {
    email,
    refreshToken,
  };
}
