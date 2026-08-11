export function TermsOfServiceContent({ platformName = "Aegis" }: { platformName?: string }) {
  const P = platformName;
  return (
    <div className="space-y-8 text-sm leading-relaxed text-foreground/90">
      {/* ── 中文版 ── */}
      <section className="space-y-5">
        <h2 className="text-lg font-bold text-foreground">用户服务协议</h2>
        <p className="text-xs text-muted-foreground">最后更新日期：2026 年 3 月 23 日</p>

        <p>
          欢迎使用 {P}（以下简称&ldquo;本平台&rdquo;）。请您在使用本平台服务之前，仔细阅读并充分理解本协议的全部内容。
          您注册、登录或以任何方式使用本平台服务，即视为您已阅读、理解并同意接受本协议的约束。
        </p>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">一、服务内容</h3>
          <p>
            本平台为用户提供多租户用户管理系统服务，包括但不限于用户认证与授权、账号管理、多因子认证、
            OAuth2 集成、角色权限控制、积分管理、签到系统、工作流引擎、存储管理、通知推送等功能。
            具体服务内容以本平台实际提供的为准，本平台保留随时变更、中断或终止部分或全部服务的权利。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">二、账号注册与安全</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>您在注册账号时应提供真实、准确、完整的信息，并在信息变更时及时更新。</li>
            <li>您应妥善保管账号和密码，对使用您的账号进行的所有活动承担责任。</li>
            <li>如发现账号存在未经授权的使用或安全漏洞，您应立即通知本平台。</li>
            <li>本平台有权对涉嫌违规的账号采取警告、限制功能、冻结或注销等措施。</li>
            <li>一个自然人仅可注册一个管理员账号，不得转让、出借或共享账号。</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">三、用户行为规范</h3>
          <p>您在使用本平台服务时，应遵守以下规范：</p>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>遵守中华人民共和国相关法律法规及本协议的各项规定。</li>
            <li>不得利用本平台从事危害国家安全、泄露国家秘密、损害国家利益的行为。</li>
            <li>不得利用本平台发布、传播违法信息或侵犯他人合法权益的内容。</li>
            <li>不得对本平台系统进行反向工程、反编译、攻击、破坏或未经授权的访问。</li>
            <li>不得利用本平台发送垃圾邮件、恶意请求或进行其他滥用行为。</li>
            <li>不得未经授权访问、收集或存储其他用户的个人信息。</li>
            <li>不得干扰或破坏本平台服务器和网络的正常运行。</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">四、知识产权</h3>
          <p>
            本平台的所有内容，包括但不限于文字、图片、代码、界面设计、商标、Logo 及其排列组合，
            均受到著作权法、商标法和其他知识产权法律法规的保护。未经本平台书面许可，
            任何人不得以任何形式复制、修改、传播或使用上述内容。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">五、数据与隐私</h3>
          <p>
            本平台重视用户的隐私保护。有关个人信息的收集、使用、存储和保护等事项，
            请参阅我们的《隐私政策》。使用本平台服务即表示您同意按照《隐私政策》的规定处理您的个人信息。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">六、服务变更与终止</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>本平台有权根据业务发展需要，修改或调整服务内容，并通过平台公告或站内通知的方式告知用户。</li>
            <li>如用户违反本协议的任何规定，本平台有权立即终止向其提供服务并追究相关责任。</li>
            <li>因不可抗力、系统维护等原因导致服务中断或终止的，本平台不承担责任，但将尽合理努力减少影响。</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">七、免责声明</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>本平台按"现状"提供服务，不对服务的可用性、准确性、可靠性作任何明示或暗示的保证。</li>
            <li>因用户自身原因（如操作失误、密码泄露等）导致的损失，本平台不承担责任。</li>
            <li>因第三方原因（如网络运营商故障、黑客攻击等）导致的服务中断或数据损失，本平台不承担责任。</li>
            <li>在法律允许的最大范围内，本平台对因使用或无法使用本服务而产生的任何间接、偶然、特殊或惩罚性损害不承担责任。</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">八、协议修改</h3>
          <p>
            本平台有权随时修改本协议，修改后的协议将在平台上公布。
            如您在协议修改后继续使用本平台服务，则视为您已接受修改后的协议。
            如您不同意修改后的协议，应停止使用本平台服务。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">九、争议解决</h3>
          <p>
            本协议的订立、执行和解释及争议的解决均应适用中华人民共和国法律。
            因本协议引起的或与本协议有关的任何争议，双方应友好协商解决；
            协商不成的，任何一方均可向本平台所在地有管辖权的人民法院提起诉讼。
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">十、联系方式</h3>
          <p>
            如您对本协议有任何疑问或建议，请通过本平台提供的联系方式与我们取得联系。
          </p>
        </div>
      </section>

      <hr className="border-border" />

      {/* ── English Version ── */}
      <section className="space-y-5">
        <h2 className="text-lg font-bold text-foreground">Terms of Service</h2>
        <p className="text-xs text-muted-foreground">Last updated: March 23, 2026</p>

        <p>
          Welcome to {P} (hereinafter referred to as &quot;the Platform&quot;). Please read and fully understand
          this Agreement before using the Platform&apos;s services. By registering, logging in, or using the Platform&apos;s
          services in any way, you acknowledge that you have read, understood, and agree to be bound by this Agreement.
        </p>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">1. Services</h3>
          <p>
            The Platform provides multi-tenant user management system services, including but not limited to user
            authentication and authorization, account management, multi-factor authentication, OAuth2 integration,
            role-based access control, points management, check-in system, workflow engine, storage management,
            and push notifications. The Platform reserves the right to modify, suspend, or terminate any or all
            services at any time.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">2. Account Registration and Security</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>You must provide accurate, complete, and up-to-date information when registering.</li>
            <li>You are responsible for maintaining the confidentiality of your account credentials and for all activities under your account.</li>
            <li>You must immediately notify the Platform of any unauthorized use or security breach.</li>
            <li>The Platform reserves the right to warn, restrict, freeze, or terminate accounts that violate these terms.</li>
            <li>Each individual may only register one administrator account and may not transfer, lend, or share it.</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">3. User Conduct</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>Comply with all applicable laws and regulations.</li>
            <li>Do not use the Platform for any illegal activities or to harm others&apos; rights.</li>
            <li>Do not reverse engineer, decompile, attack, or gain unauthorized access to the Platform.</li>
            <li>Do not send spam, make malicious requests, or engage in any abusive behavior.</li>
            <li>Do not collect or store other users&apos; personal information without authorization.</li>
            <li>Do not interfere with or disrupt the Platform&apos;s servers or network operations.</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">4. Intellectual Property</h3>
          <p>
            All content on the Platform, including text, images, code, interface design, trademarks, logos, and their
            arrangement, is protected by copyright, trademark, and other intellectual property laws. No content may
            be reproduced, modified, distributed, or used without the Platform&apos;s written permission.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">5. Data and Privacy</h3>
          <p>
            The Platform values your privacy. Please refer to our Privacy Policy for details on how we collect,
            use, store, and protect your personal information. By using the Platform, you consent to data processing
            as described in the Privacy Policy.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">6. Service Changes and Termination</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>The Platform may modify services and will notify users through platform announcements.</li>
            <li>The Platform may terminate services to users who violate this Agreement.</li>
            <li>The Platform is not liable for interruptions caused by force majeure or system maintenance.</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">7. Disclaimer</h3>
          <ol className="list-decimal pl-5 space-y-1.5">
            <li>Services are provided &quot;as is&quot; without warranties of any kind.</li>
            <li>The Platform is not liable for losses caused by user error or credential compromise.</li>
            <li>The Platform is not liable for disruptions caused by third parties such as ISPs or hackers.</li>
            <li>To the maximum extent permitted by law, the Platform shall not be liable for any indirect, incidental, special, or punitive damages.</li>
          </ol>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">8. Amendments</h3>
          <p>
            The Platform may amend this Agreement at any time. Continued use of the Platform after amendments
            constitutes acceptance of the revised terms. If you disagree, please discontinue use.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">9. Governing Law</h3>
          <p>
            This Agreement shall be governed by and construed in accordance with the laws of the People&apos;s Republic
            of China. Any disputes arising from this Agreement shall be resolved through friendly negotiation;
            failing which, either party may submit the dispute to the competent court in the jurisdiction where
            the Platform is located.
          </p>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold text-foreground">10. Contact Us</h3>
          <p>
            If you have any questions or suggestions about this Agreement, please contact us through the
            channels provided on the Platform.
          </p>
        </div>
      </section>
    </div>
  );
}
