# frozen_string_literal: true

# name: connect-identity
# about: Signed identity bridge between Discourse and XAI Connect
# version: 0.2.0
# authors: XAI.RUN
# url: https://connect.xai.run

enabled_site_setting :connect_identity_enabled

register_asset "stylesheets/connect_identity.scss"

after_initialize do
  require_relative "lib/connect_identity/configuration"
  require_relative "lib/connect_identity/signed_request"
  require_relative "lib/connect_identity/publisher"
  require_relative "jobs/regular/connect_identity_publish_status"
  require_relative "app/controllers/connect_identity/base_controller"
  require_relative "app/controllers/connect_identity/users_controller"

  unless ConnectIdentity::Configuration.valid?
    Rails.logger.warn("connect-identity is disabled because its HTTPS base URL or shared secret is not configured")
    next
  end

  Discourse::Application.routes.append do
    get "/connect/identity/users/:id" => "connect_identity/users#show"
  end

  on(:user_updated) do |user|
    ConnectIdentity::Publisher.enqueue(user)
  end
end
