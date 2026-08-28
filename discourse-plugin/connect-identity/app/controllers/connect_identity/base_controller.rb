# frozen_string_literal: true

class ConnectIdentity::BaseController < ::ApplicationController
  include ConnectIdentity::SignedRequest

  skip_before_action :verify_authenticity_token
  before_action :verify_connect_request!

  private

  def render_minimal_user(user)
    render json: ConnectIdentity::Publisher::UserSerializer.new(user).status_fields
  end
end
