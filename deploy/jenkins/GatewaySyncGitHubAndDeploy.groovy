pipeline {
  agent any

  options {
    timestamps()
    disableConcurrentBuilds()
    skipDefaultCheckout(true)
    buildDiscarder(logRotator(numToKeepStr: '20'))
  }

  parameters {
    string(name: 'BRANCH', defaultValue: 'master', description: 'GitHub/NAS 同步只允许 master。')
    booleanParam(name: 'DEPLOY_WHEN_UNCHANGED', defaultValue: false, description: '两端已一致时，是否仍触发一次 Gateway 发布。')
  }

  environment {
    PRODUCTION_BRANCH = 'master'
    GITHUB_REPO = 'https://github.com/chenbb0128/WeavePress.git'
    GITHUB_CREDENTIALS_ID = 'github-nas-mirror'
    GITHUB_HTTP_PROXY = 'http://192.168.31.227:7890'
    NAS_REPO = 'ssh://chenhua@192.168.31.240/volume1/docker/weavepress-git/WeavePress.git'
    NAS_GIT_SSH_COMMAND = 'ssh -i /var/jenkins_home/.ssh/nas_classmate_git_ed25519 -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/jenkins_home/.ssh/known_hosts'
    DIRECT_DEPLOY_JOB = 'WeavePress/WeavePressGateway'
  }

  stages {
    stage('校验固定生产分支') {
      steps {
        script {
          if (params.BRANCH != env.PRODUCTION_BRANCH) {
            error('GitHub/NAS 同步只允许 master。')
          }
        }
      }
    }

    stage('比较并按需同步 GitHub master 到 NAS') {
      steps {
        deleteDir()
        withCredentials([usernamePassword(credentialsId: env.GITHUB_CREDENTIALS_ID, usernameVariable: 'GITHUB_USER', passwordVariable: 'GITHUB_TOKEN')]) {
          sh '''#!/usr/bin/env bash
            set -Eeuo pipefail
            set +x
            [ "${BRANCH:-}" = master ] || { echo '同步只允许 master。' >&2; exit 64; }
            export HTTP_PROXY="$GITHUB_HTTP_PROXY" HTTPS_PROXY="$GITHUB_HTTP_PROXY"
            export NO_PROXY='127.0.0.1,localhost,192.168.31.240'
            export no_proxy="$NO_PROXY"
            export GIT_HTTP_LOW_SPEED_LIMIT=1 GIT_HTTP_LOW_SPEED_TIME=600
            askpass="$PWD/.git-askpass.sh"
            cleanup() {
              rm -f -- "$askpass"
            }
            trap cleanup EXIT
            umask 077
            printf '%s\n' '#!/bin/sh' \
              'case "$1" in' \
              '  *Username*) printf "%s\\n" "$GITHUB_USER" ;;' \
              '  *) printf "%s\\n" "$GITHUB_TOKEN" ;;' \
              'esac' > "$askpass"
            chmod 700 "$askpass"
            export GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0

            github_sha="$(git -c http.proxy="$GITHUB_HTTP_PROXY" ls-remote "$GITHUB_REPO" refs/heads/master | awk 'NR == 1 { print $1 } END { if (NR != 1) exit 1 }')"
            printf '%s' "$github_sha" | grep -Eq '^[0-9a-f]{40}$' || {
              echo 'GitHub master 未返回唯一、有效的 SHA。' >&2
              exit 65
            }
            nas_sha="$(GIT_SSH_COMMAND="$NAS_GIT_SSH_COMMAND" git ls-remote "$NAS_REPO" refs/heads/master | awk 'NR == 1 { print $1 } END { if (NR > 1) exit 1 }')"
            if [ -n "$nas_sha" ]; then
              printf '%s' "$nas_sha" | grep -Eq '^[0-9a-f]{40}$' || {
                echo 'NAS master SHA 格式无效。' >&2
                exit 65
              }
            fi

            if [ "$github_sha" = "$nas_sha" ]; then
              printf 'UNCHANGED=true\nSYNCED_COMMIT=%s\n' "$github_sha" > sync-result.env
              exit 0
            fi

            git clone --branch master --single-branch --no-tags "$GITHUB_REPO" sync-source
            actual_sha="$(git -C sync-source rev-parse HEAD)"
            test "$actual_sha" = "$github_sha"
            git -C sync-source remote add nas "$NAS_REPO"
            (
              cd sync-source
              GIT_SSH_COMMAND="$NAS_GIT_SSH_COMMAND" git push nas HEAD:refs/heads/master
            )
            printf 'UNCHANGED=false\nSYNCED_COMMIT=%s\n' "$actual_sha" > sync-result.env
          '''
        }
      }
    }

    stage('仅在代码未变化时按需触发 Gateway') {
      steps {
        script {
          def result = readFile('sync-result.env').readLines()
          def unchanged = result.find { it.startsWith('UNCHANGED=') }?.substring('UNCHANGED='.length())
          def synced = result.find { it.startsWith('SYNCED_COMMIT=') }?.substring('SYNCED_COMMIT='.length()) ?: ''
          if (!(unchanged in ['true', 'false']) || !(synced ==~ /[0-9a-f]{40}/) || result.size() != 2) {
            error('同步结果格式无效。')
          }
          currentBuild.description = "master=${synced} unchanged=${unchanged}"
          if (unchanged == 'false') {
            echo 'GitHub master 已精确推送到 NAS；后续只由 NAS hook 分发，Sync Pipeline 不重复触发直接发布。'
            return
          }
          if (!params.DEPLOY_WHEN_UNCHANGED) {
            echo 'GitHub 与 NAS 已一致，按参数跳过发布。'
            return
          }
          build job: env.DIRECT_DEPLOY_JOB, wait: false, parameters: [
            string(name: 'BRANCH', value: 'master'),
            string(name: 'APP_SHA', value: synced),
            booleanParam(name: 'PUSH_ACR', value: true),
            booleanParam(name: 'DEPLOY', value: true),
            string(name: 'TRIGGER_REPO', value: 'gateway-sync')
          ]
        }
      }
    }
  }

  post {
    always {
      deleteDir()
    }
  }
}
