export function PrivacyPolicyContent({ platformName = "Aegis" }: { platformName?: string }) {
  const P = platformName;
  return (
    <div className="space-y-8 text-sm leading-relaxed text-foreground/90">
      {/* ── 中文版 ── */}
      <section className="space-y-5">
        <h2 className="text-lg font-bold text-foreground">隐私政策</h2>
        <p className="text-xs text-muted-foreground">最后更新日期：2026 年 3 月 23 日</p>

        <p>
          {P}（以下简称&ldquo;本平台&rdquo;）深知个人信息对您的重要性，并会尽全力保护您的个人信息安全。
          本隐私政策说明我们如何收集、使用、存储、共享和保护您的个人信息。
          请您在使用本平台服务前，仔细阅读并理解本政策。
        </p>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">一、我们收集的信息</h3>
          <p>为了向您提供服务，我们可能会收集以下类型的信息：</p>

          <div className="space-y-2">
            <h4 className="text-sm font-medium text-foreground">1.1 您主动提供的信息</h4>
            <ul className="list-disc pl-5 space-y-1">
              <li><strong>账号信息</strong>：用户名、显示名称、电子邮箱地址</li>
              <li><strong>认证信息</strong>：密码（加密存储）、多因子认证凭据（TOTP 密钥、Passkey）、恢复码</li>
              <li><strong>个人资料</strong>：头像、个人描述等您选择填写的信息</li>
            </ul>
          </div>

          <div className="space-y-2">
            <h4 className="text-sm font-medium text-foreground">1.2 自动收集的信息</h4>
            <ul className="list-disc pl-5 space-y-1">
              <li><strong>IP 地址</strong>：用于安全审计、地理位置分析、异常登录检测和 IP 封禁</li>
              <li><strong>地理位置信息</strong>：基于 IP 地址解析的国家、城市、经纬度（使用 GeoIP2 数据库，精度为城市级）</li>
              <li><strong>设备信息</strong>：浏览器类型（User-Agent）、操作系统、屏幕分辨率</li>
              <li><strong>会话信息</strong>：登录时间、会话 Token、活跃状态、最后访问时间</li>
              <li><strong>网络信息</strong>：ASN（自治系统号）、ISP 信息</li>
              <li><strong>请求信息</strong>：请求路径、请求方法、时间戳、Nonce（防重放攻击）</li>
              <li><strong>安全事件</strong>：防火墙拦截记录、异常请求日志、WAF 规则命中详情</li>
            </ul>
          </div>

          <div className="space-y-2">
            <h4 className="text-sm font-medium text-foreground">1.3 第三方登录信息</h4>
            <p>
              当您使用第三方账号（QQ、微信、GitHub、Google、Microsoft、微博）登录时，
              我们会获取该第三方平台授权的基本信息（如昵称、头像、唯一标识符），
              具体范围取决于您在第三方平台的授权设置。
            </p>
          </div>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">二、我们如何使用信息</h3>
          <p>我们收集的信息将用于以下目的：</p>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>提供核心服务</strong>：账号注册与登录、身份验证、权限管理、多因子认证</li>
            <li><strong>安全保障</strong>：异常登录检测、IP 风控、防火墙规则执行、IP 封禁管理、防重放攻击</li>
            <li><strong>审计与合规</strong>：登录日志记录、安全事件追踪、操作审计</li>
            <li><strong>服务优化</strong>：系统监控、性能分析、故障排查</li>
            <li><strong>用户体验</strong>：个性化设置、通知推送、签到与积分功能</li>
            <li><strong>安全通信</strong>：发送安全验证码、密码重置邮件、账号异常通知</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">三、信息存储与保护</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>存储位置</strong>：您的个人信息存储在本平台部署的服务器中，数据库使用 PostgreSQL 并配置了连接加密。</li>
            <li><strong>密码安全</strong>：用户密码使用 bcrypt 算法进行单向加密存储，我们无法获知您的明文密码。</li>
            <li><strong>会话安全</strong>：会话 Token 使用 JWT（HS256 签名）生成，Redis 存储会话状态，支持远程注销。</li>
            <li><strong>传输加密</strong>：建议部署 HTTPS/TLS 加密传输，防止数据在传输过程中被窃取。</li>
            <li><strong>访问控制</strong>：严格的 RBAC 权限控制（基于 Casbin），管理员操作需要多因子认证。</li>
            <li><strong>安全防护</strong>：集成 WAF（Coraza + OWASP CRS）、限流、防重放攻击等多层安全机制。</li>
            <li><strong>存储期限</strong>：我们将在实现服务目的所必需的最短期限内保留您的个人信息。账号注销后，我们将在合理期限内删除或匿名化处理您的个人信息。</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">四、信息共享与披露</h3>
          <p>我们不会向第三方出售您的个人信息。在以下情况下，我们可能会共享或披露您的信息：</p>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>法律要求</strong>：根据法律法规、司法程序或政府机关的强制性要求。</li>
            <li><strong>用户授权</strong>：经您明确同意后，向您指定的第三方共享。</li>
            <li><strong>安全需要</strong>：为保护本平台、用户或公众的权益、财产或安全。</li>
            <li><strong>业务转让</strong>：在合并、收购或资产转让情况下，您的信息可能作为交易的一部分被转移，我们将确保接收方遵守本隐私政策。</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">五、Cookie 与本地存储</h3>
          <p>
            本平台使用浏览器本地存储（localStorage）保存认证 Token 和用户偏好设置。
            我们不使用第三方追踪 Cookie。您可以通过浏览器设置管理或删除本地存储数据，
            但这可能影响您正常使用本平台的部分功能。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">六、您的权利</h3>
          <p>您对自己的个人信息享有以下权利：</p>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>访问权</strong>：您可以查看和访问您的个人资料、登录历史和会话信息。</li>
            <li><strong>更正权</strong>：您可以修改您的账号信息、显示名称、邮箱等个人资料。</li>
            <li><strong>删除权</strong>：您可以申请注销账号，我们将在合理期限内删除您的个人信息。</li>
            <li><strong>撤回同意</strong>：您可以随时撤回对个人信息收集的同意，但这可能影响我们向您提供服务。</li>
            <li><strong>会话管理</strong>：您可以查看所有活跃会话并远程注销不需要的会话。</li>
            <li><strong>数据导出</strong>：您有权请求导出您的个人数据。</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">七、未成年人保护</h3>
          <p>
            本平台的服务主要面向成年用户。如果您是未满 18 周岁的未成年人，请在监护人的陪同和指导下阅读本政策，
            并在取得监护人同意后使用本平台服务。如果我们发现在未获得监护人同意的情况下收集了未成年人的个人信息，
            我们将尽快删除相关信息。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">八、政策更新</h3>
          <p>
            我们可能会不时修订本隐私政策。更新后的政策将在本平台上发布，并标注最新更新日期。
            对于重大变更，我们会通过平台公告或站内通知的方式提醒您。
            继续使用本平台服务即表示您同意受修订后的政策约束。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">九、联系我们</h3>
          <p>
            如您对本隐私政策有任何疑问、意见或建议，或需要行使您的个人信息权利，
            请通过本平台提供的联系方式与我们取得联系，我们将在合理期限内予以回复。
          </p>
        </div>
      </section>

      <hr className="border-border" />

      {/* ── English Version ── */}
      <section className="space-y-5">
        <h2 className="text-lg font-bold text-foreground">Privacy Policy</h2>
        <p className="text-xs text-muted-foreground">Last updated: March 23, 2026</p>

        <p>
          {P} (hereinafter referred to as &quot;the Platform&quot;) understands the importance of your
          personal information and is committed to protecting it. This Privacy Policy explains how we collect,
          use, store, share, and protect your personal information.
        </p>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">1. Information We Collect</h3>

          <div className="space-y-2">
            <h4 className="text-sm font-medium text-foreground">1.1 Information You Provide</h4>
            <ul className="list-disc pl-5 space-y-1">
              <li><strong>Account information</strong>: username, display name, email address</li>
              <li><strong>Authentication data</strong>: password (encrypted), MFA credentials (TOTP keys, Passkeys), recovery codes</li>
              <li><strong>Profile data</strong>: avatar, bio, and other optional information</li>
            </ul>
          </div>

          <div className="space-y-2">
            <h4 className="text-sm font-medium text-foreground">1.2 Information Collected Automatically</h4>
            <ul className="list-disc pl-5 space-y-1">
              <li><strong>IP address</strong>: for security auditing, geolocation, anomaly detection, and IP blocking</li>
              <li><strong>Geolocation</strong>: country, city, and approximate coordinates derived from IP (city-level accuracy via GeoIP2)</li>
              <li><strong>Device information</strong>: browser type (User-Agent), operating system, screen resolution</li>
              <li><strong>Session data</strong>: login time, session token, activity status, last access time</li>
              <li><strong>Network information</strong>: ASN (Autonomous System Number), ISP details</li>
              <li><strong>Request data</strong>: request path, method, timestamp, nonce (anti-replay)</li>
              <li><strong>Security events</strong>: firewall blocks, suspicious request logs, WAF rule matches</li>
            </ul>
          </div>

          <div className="space-y-2">
            <h4 className="text-sm font-medium text-foreground">1.3 Third-Party Login Information</h4>
            <p>
              When you log in via third-party services (QQ, WeChat, GitHub, Google, Microsoft, Weibo),
              we receive basic profile information authorized by those platforms (e.g., nickname, avatar, unique ID).
              The scope depends on your authorization settings on the third-party platform.
            </p>
          </div>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">2. How We Use Information</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>Core services</strong>: account registration, login, identity verification, permission management, MFA</li>
            <li><strong>Security</strong>: anomaly detection, IP risk control, firewall enforcement, IP banning, anti-replay protection</li>
            <li><strong>Audit and compliance</strong>: login logging, security event tracking, operation auditing</li>
            <li><strong>Service improvement</strong>: system monitoring, performance analysis, troubleshooting</li>
            <li><strong>User experience</strong>: personalized settings, notifications, check-in and points features</li>
            <li><strong>Communications</strong>: security verification codes, password reset emails, account alerts</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">3. Data Storage and Protection</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>Storage</strong>: data is stored on servers deployed by the Platform, using PostgreSQL with encrypted connections.</li>
            <li><strong>Passwords</strong>: hashed with bcrypt (one-way encryption); we cannot access your plaintext password.</li>
            <li><strong>Sessions</strong>: JWT tokens (HS256 signed) with Redis session state, supporting remote logout.</li>
            <li><strong>Encryption</strong>: HTTPS/TLS is recommended for all data in transit.</li>
            <li><strong>Access control</strong>: strict RBAC (Casbin-based) with MFA required for admin operations.</li>
            <li><strong>Security</strong>: WAF (Coraza + OWASP CRS), rate limiting, anti-replay, and multi-layer defense.</li>
            <li><strong>Retention</strong>: we retain data for the minimum period necessary. After account deletion, data is removed or anonymized within a reasonable timeframe.</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">4. Information Sharing and Disclosure</h3>
          <p>We do not sell your personal information. We may share your information only when:</p>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>Legal requirements</strong>: compelled by law, court order, or government authority.</li>
            <li><strong>Your consent</strong>: shared with third parties you explicitly authorize.</li>
            <li><strong>Security</strong>: necessary to protect the Platform, users, or the public.</li>
            <li><strong>Business transfers</strong>: in the event of a merger, acquisition, or asset sale, with the successor bound by this policy.</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">5. Cookies and Local Storage</h3>
          <p>
            The Platform uses browser localStorage to store authentication tokens and user preferences.
            We do not use third-party tracking cookies. You may manage or delete local storage data through
            your browser settings, but this may affect Platform functionality.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">6. Your Rights</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li><strong>Access</strong>: view your profile, login history, and session information.</li>
            <li><strong>Correction</strong>: update your account details, display name, and email.</li>
            <li><strong>Deletion</strong>: request account deletion; we will remove your data within a reasonable period.</li>
            <li><strong>Withdraw consent</strong>: you may withdraw consent at any time, though this may affect service availability.</li>
            <li><strong>Session management</strong>: view all active sessions and remotely revoke unwanted ones.</li>
            <li><strong>Data portability</strong>: you may request an export of your personal data.</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">7. Children&apos;s Privacy</h3>
          <p>
            The Platform is intended for adult users. If you are under 18, please read this policy with a
            guardian and obtain their consent before using the Platform. If we discover that we have collected
            personal information from a minor without guardian consent, we will delete it promptly.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">8. Policy Updates</h3>
          <p>
            We may update this Privacy Policy periodically. The updated version will be posted on the Platform
            with the latest revision date. For significant changes, we will notify you through platform
            announcements. Continued use of the Platform constitutes acceptance of the revised policy.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">9. Contact Us</h3>
          <p>
            If you have any questions, comments, or requests regarding this Privacy Policy or wish to
            exercise your rights, please contact us through the channels provided on the Platform.
          </p>
        </div>
      </section>
    </div>
  );
}
