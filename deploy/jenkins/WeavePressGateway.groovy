pipeline {
  agent any

  options {
    timestamps()
    timeout(time: 45, unit: 'MINUTES')
    disableConcurrentBuilds()
    skipDefaultCheckout(true)
    buildDiscarder(logRotator(numToKeepStr: '20'))
  }

  parameters {
    string(name: 'BRANCH', defaultValue: 'master', description: '生产构建只允许 master。')
    string(name: 'APP_SHA', defaultValue: '', description: '必须是 master 上完整的 40 位小写 commit SHA。')
    booleanParam(name: 'PUSH_ACR', defaultValue: false, description: '将本次精确 SHA 的 Gateway 镜像推送到 ACR。')
    booleanParam(name: 'DEPLOY', defaultValue: false, description: '推送镜像后通过受限 SSH 发布 Gateway。')
    string(name: 'TRIGGER_REPO', defaultValue: 'manual', description: '固定审计来源。')
  }

  environment {
    NAS_REPO = 'ssh://chenhua@192.168.31.240/volume1/docker/weavepress-git/WeavePress.git'
    NAS_GIT_SSH_COMMAND = 'ssh -i /var/jenkins_home/.ssh/nas_classmate_git_ed25519 -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/jenkins_home/.ssh/known_hosts'
    ACR_REGISTRY = 'registry.cn-hangzhou.aliyuncs.com'
    PROD_HOST = '124.220.53.160'
    PROD_PORT = '22'
    PROD_USER = 'weavepress-deploy'
    PROD_SSH_CREDENTIALS_ID = 'weavepress-tencent-prod-ssh'
  }

  stages {
    stage('校验参数并检出精确 Gateway 版本') {
      steps {
        deleteDir()
        sh '''#!/usr/bin/env bash
          set -Eeuo pipefail
          [ "${BRANCH:-}" = master ] || { echo '生产构建只允许 master。' >&2; exit 64; }
          printf '%s' "${APP_SHA:-}" | grep -Eq '^[0-9a-f]{40}$' || {
            echo 'APP_SHA 必须是完整的 40 位小写 commit SHA。' >&2
            exit 64
          }
          printf '%s' "${TRIGGER_REPO:-}" | grep -Eq '^(manual|nas-hook|gateway-sync)$' || {
            echo 'TRIGGER_REPO 不在允许列表中。' >&2
            exit 64
          }
          export GIT_SSH_COMMAND="$NAS_GIT_SSH_COMMAND"
          git clone --no-checkout --branch master --single-branch "$NAS_REPO" source
          git -C source cat-file -e "$APP_SHA^{commit}"
          git -C source merge-base --is-ancestor "$APP_SHA" origin/master
          master_sha="$(git -C source rev-parse origin/master)"
          test "$master_sha" = "$APP_SHA"
          git -C source checkout --detach "$APP_SHA"
          actual_sha="$(git -C source rev-parse HEAD)"
          test "$actual_sha" = "$APP_SHA"
          test -f source/deploy/Dockerfile.gateway
          printf '%s\n' "$actual_sha" > APP_SHA_RESOLVED
        '''
        script {
          def resolved = readFile('APP_SHA_RESOLVED').trim()
          if (!(resolved ==~ /[0-9a-f]{40}/) || resolved != params.APP_SHA) {
            error('检出结果与 APP_SHA 不一致。')
          }
          currentBuild.displayName = "#${env.BUILD_NUMBER} Gateway ${resolved.take(8)}"
          currentBuild.description = "APP_SHA=${resolved} trigger=${params.TRIGGER_REPO}"
        }
      }
    }

    stage('构建 Gateway 不可变镜像') {
      steps {
        sh '''#!/usr/bin/env bash
          set -Eeuo pipefail
          docker build -f source/deploy/Dockerfile.gateway --build-arg APP_SHA=$APP_SHA -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:$APP_SHA source
          docker image inspect "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:$APP_SHA" >/dev/null
        '''
      }
    }

    stage('推送 Gateway 镜像到 ACR') {
      when {
        anyOf {
          expression { return params.PUSH_ACR == true }
          expression { return params.DEPLOY == true }
        }
      }
      steps {
        withCredentials([usernamePassword(credentialsId: 'aliyun-acr-zdzq', usernameVariable: 'ACR_USER', passwordVariable: 'ACR_PASSWORD')]) {
          sh(label: '推送不可变镜像（最多三次）', script: '''#!/usr/bin/env bash
            set -Eeuo pipefail
            set +x
            docker_config="$(mktemp -d)"
            export DOCKER_CONFIG="$docker_config"
            cleanup() {
              docker logout registry.cn-hangzhou.aliyuncs.com >/dev/null 2>&1 || true
              rm -rf -- "$docker_config"
            }
            trap cleanup EXIT
            printf '%s\n' "$ACR_PASSWORD" | docker login --username "$ACR_USER" --password-stdin registry.cn-hangzhou.aliyuncs.com >/dev/null
            ACR_PASSWORD=''
            mkdir -p manifest-digests
            bash source/deploy/jenkins/publish-immutable-image.sh \
              "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:$APP_SHA" \
              "$APP_SHA" manifest-digests/gateway.digest
          ''')
        }
        archiveArtifacts artifacts: 'manifest-digests/*.digest', fingerprint: true
      }
    }

    // The forced command returns success only after the Gateway image SHA
    // is active behind the production Nginx layer.
    stage('发布并确认 Gateway image SHA') {
      when { expression { return params.DEPLOY == true } }
      steps {
        withCredentials([usernamePassword(credentialsId: 'aliyun-acr-zdzq', usernameVariable: 'ACR_USER', passwordVariable: 'ACR_PASSWORD')]) {
          sshagent(credentials: [env.PROD_SSH_CREDENTIALS_ID]) {
            sh '''#!/usr/bin/env bash
              set -Eeuo pipefail
              set +x
              gateway_digest="$(<manifest-digests/gateway.digest)"
              printf '%s' "$gateway_digest" | grep -Eq '^sha256:[0-9a-f]{64}$'
              printf '%s\n%s\n%s\n' "$ACR_USER" "$ACR_PASSWORD" "$gateway_digest" | ssh \
                -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes \
                -o UserKnownHostsFile=/var/jenkins_home/.ssh/known_hosts \
                -o ServerAliveInterval=30 -o ServerAliveCountMax=30 \
                -p "$PROD_PORT" "$PROD_USER@$PROD_HOST" \
                "$APP_SHA --component=gateway"
            '''
          }
        }
      }
    }

    stage('验证两层 Nginx 公网入口') {
      when { expression { return params.DEPLOY == true } }
      steps {
        sh '''#!/usr/bin/env bash
          set -Eeuo pipefail
          bash source/deploy/jenkins/verify-gateway-release.sh "$APP_SHA" \
            https://wp.pdurl.cn/ready https://wp.pdurl.cn/
        '''
      }
    }
  }

  post {
    always {
      deleteDir()
    }
  }
}
